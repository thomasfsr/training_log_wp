package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
	"strconv"

	"github.com/joho/godotenv"
	"github.com/openai/openai-go/v2"
	"github.com/openai/openai-go/v2/option"
	_ "modernc.org/sqlite"
)

func LLMMessageClassifier(user_id uint64, user_input string) *OverallState {
	_ = godotenv.Load()
	groq_key := os.Getenv("GROQ_API_KEY")
	client := openai.NewClient(
		option.WithAPIKey(groq_key),
		option.WithBaseURL("https://api.groq.com/openai/v1"),
	)
	ctx := context.Background()
	ModelName := "moonshotai/kimi-k2-instruct-0905"

	schemaParam := openai.ResponseFormatJSONSchemaJSONSchemaParam{
		Name:        "Classifier",
		Description: openai.String("Classify the user input to either of three categories: Chat, Query, Insert"),
		Schema:      CategorySchema,
		Strict:      openai.Bool(true),
	}
	chat, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(
				`You should classifier the user input in either: query, insert and chat.
				- Insert: If the user input is data of workout such as exercise, reps weight and sets.
				- Query: If the user input requests information about the workout data.
				- Chat: If neither of insert or query.`),
			openai.UserMessage(user_input),
		},
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &openai.ResponseFormatJSONSchemaParam{JSONSchema: schemaParam},
		},
		Model: ModelName,
	})

	if err != nil {
		panic(err.Error())
	}
	chat_response_content := &chat.Choices[0].Message.Content
	fmt.Println(*chat_response_content)
  var category Category
	err = json.Unmarshal([]byte(*chat_response_content), &category)
	if err != nil {
		panic(err.Error())
	}
	Role := "user"
	state := OverallState{
		Messages : []Message{
			{Role: Role, Content: user_input},
		},
		Category: category.category,
		UserID: user_id,
		UserInput: user_input,
	}
	return &state
}

func LLMChat(db *sql.DB, state *OverallState) {
	_ = godotenv.Load()
	groq_key := os.Getenv("GROQ_API_KEY")
	client := openai.NewClient(
		option.WithAPIKey(groq_key),
		option.WithBaseURL("https://api.groq.com/openai/v1"),
	)
	ctx := context.Background()
	const n_past_messages uint8 = 10
	conversationHistory := ConversationHistory(db, state.UserID, n_past_messages)
	fmt.Printf("\n\nconversation history: %v\n\n", conversationHistory)
	ModelName := "llama-3.3-70b-versatile"

	Messages := []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(
				`You are a helpful assistant of a fitness app. You should inform the user that he can give the informations of:
				exercise name, sets, reps and weight. Obs: I will give you the 10 previous messages as context:`),
			
	}
	Messages = append(Messages, *conversationHistory...)
	Messages = append(Messages, openai.UserMessage(strings.ToLower(state.UserInput)))

	chat, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: ModelName,
		Messages: Messages,
	})
	if err != nil {
		panic(err.Error())
	}
	chat_response_content := &chat.Choices[0].Message.Content
	state.Messages = append(state.Messages,Message{Role: "assistant", Content: *chat_response_content}) 
}

func LLMQueryData(db *sql.DB, state *OverallState) {
		user_input := state.UserInput
	user_id := state.UserID
	_ = godotenv.Load()
	groq_key := os.Getenv("GROQ_API_KEY")
	client := openai.NewClient(
		option.WithAPIKey(groq_key),
		option.WithBaseURL("https://api.groq.com/openai/v1"),
	)
	ctx := context.Background()
	ModelName := "moonshotai/kimi-k2-instruct-0905"

	systemMsg := fmt.Sprintf(`You are a SQL query generator for a fitness app database.
Always include "user_id = %d" in the WHERE clause of your queries.
Translate user requests into appropriate SQL statements. 
IMPORTANT the schema of the sqlite table 'workout_sets' you should use is:
SQLite -> table: workout_sets (
id INTEGER PRIMARY KEY AUTOINCREMENT, 
user_id INTEGER,
exercise CHAR(100),
weight INTEGER,
reps INTEGER,
created_at TIMESTAMP
);
CRITICAL: Return ONLY the SQL query, no markdown formatting, no code blocks, no explanations.`, user_id)

	chat, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(systemMsg),
			openai.UserMessage(user_input),
		},
		Model: ModelName,
	})
	if err != nil {
		panic(err.Error())
	}
	chat_response_content := &chat.Choices[0].Message.Content

	cleanedSQL := cleanSQLResponse(*chat_response_content)
	fmt.Printf("Generated SQL: %s\n", cleanedSQL)

	tx, err := db.Begin()

	if err != nil {
		fmt.Printf("Error in llm query data: %v", err.Error())
		return
	}
	defer tx.Rollback() 
	rows, err := tx.Query(cleanedSQL)
	if err != nil {
		fmt.Printf("LLM Error: %v", err.Error())
		return
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		fmt.Printf("LLM Error: %v", err.Error())
		return
	}
	var resultStrings []string
	resultStrings = append(resultStrings, "Query Results:")
	resultStrings = append(resultStrings, strings.Join(columns, " | "))
	for rows.Next() {
    values := make([]any, len(columns))
    valuePtrs := make([]any, len(columns))
    for i := range columns {
        valuePtrs[i] = &values[i]
    }
    if err := rows.Scan(valuePtrs...); err != nil {
        fmt.Printf("Row scan error: %s\n", err.Error())
        continue
    }
    var rowStrings []string
    for _, val := range values {
        switch v := val.(type) {
        case nil:
            rowStrings = append(rowStrings, "NULL")
        case []byte:
            rowStrings = append(rowStrings, string(v))
        case int64:
            rowStrings = append(rowStrings, strconv.FormatInt(v, 10))
        case float64:
            rowStrings = append(rowStrings, strconv.FormatFloat(v, 'f', 2, 64))
        case bool:
            rowStrings = append(rowStrings, strconv.FormatBool(v))
        case time.Time:
            rowStrings = append(rowStrings, v.Format("2006-01-02"))
        default:
            rowStrings = append(rowStrings, fmt.Sprintf("%v", v))
        }
    }
    resultStrings = append(resultStrings, strings.Join(rowStrings, " | "))
}
	rawResult := strings.Join(resultStrings, "\n")
	chat, err = client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(`You are responsable of read the user query, 
the output and respond to the user based on the data collected. 
Just give the answer, no need of flags, label or anything else.`),
			openai.UserMessage(user_input),
			openai.SystemMessage(rawResult),
		},
		Model: ModelName,
	})
	if err != nil {
		panic(err.Error())
	}
	chat_response_content = &chat.Choices[0].Message.Content
	state.Messages = append(state.Messages, Message{Role: "assistant", Content: *chat_response_content })
}

func LLMInsert(db *sql.DB, state *OverallState) {
	user_input := state.UserInput
	_ = godotenv.Load()
	groq_key := os.Getenv("GROQ_API_KEY")
	client := openai.NewClient(
		option.WithAPIKey(groq_key),
		option.WithBaseURL("https://api.groq.com/openai/v1"),
	)
	ctx := context.Background()
	ModelName := "moonshotai/kimi-k2-instruct-0905"

	schemaParam := openai.ResponseFormatJSONSchemaJSONSchemaParam{
		Name:        "exercises",
		Description: openai.String("Exercises extracted from users input"),
		Schema:      ListOfExercisesSchema,
		Strict:      openai.Bool(true),
	}
	chat, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(
				`You should parse the user input to extract information  
about workout session. You should indentify the exercise(s)
sets, each set has its own reps and weight.`),
			openai.UserMessage(user_input),
		},
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &openai.ResponseFormatJSONSchemaParam{JSONSchema: schemaParam},
		},
		Model: ModelName,
	})

	if err != nil {
		panic(err.Error())
	}
	chat_response_content := &chat.Choices[0].Message.Content
	fmt.Println(*chat_response_content)

	err = json.Unmarshal([]byte(*chat_response_content), &state.ExerciseList)
	if err != nil {
		panic(err.Error())
	}
	InsertOverallState(db, state)
	state.Messages = append(state.Messages, Message{Role: "assistant", Content: "workout saved"})
}



func LLMRouteInput(db *sql.DB, state *OverallState) {
	if state.Category == "insert" {
		LLMInsert(db, state)
	} 
	if state.Category == "query" {
		LLMQueryData(db, state)
	}
	if state.Category == "chat" {
		LLMChat(db, state)
	}
}
