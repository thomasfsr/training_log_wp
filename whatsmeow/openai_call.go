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

	"github.com/invopop/jsonschema"
	"github.com/joho/godotenv"
	"github.com/openai/openai-go/v2"
	"github.com/openai/openai-go/v2/option"
	_ "modernc.org/sqlite"
)

func GenerateSchema[T any]() *jsonschema.Schema {
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties: false,
		DoNotReference:            true,
	}
	var v T
	schema := reflector.Reflect(v)
	return schema
}

var ListOfExercisesSchema = GenerateSchema[ListOfExercises]()

func cleanSQLResponse(sql string) string {
	cleaned := strings.TrimPrefix(sql, "```sql")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)
	return cleaned
}

func ConversationHistory(db *sql.DB, user_id uint64, n_msgs uint8) *[]openai.ChatCompletionMessageParamUnion {
	tx, err := db.Begin()
	if err != nil {
		fmt.Printf("Error to connect to db: %v", err.Error())
		return nil
	}
	defer tx.Rollback()
	rows, err := tx.Query("SELECT role, message FROM messages WHERE user_id = ? ORDER BY id ASC LIMIT ?", user_id, n_msgs)
	if err != nil {
		fmt.Printf("Error querying previous messages: %v", err.Error())
		return nil
	}
	defer rows.Close()
	var conversationHistory []openai.ChatCompletionMessageParamUnion

	for rows.Next() {
		var role, message string
		err := rows.Scan(&role, &message)
		if err != nil {
			fmt.Printf("Error scanning message: %v", err.Error()) 
			continue}
		switch role {
		case "user":
			conversationHistory = append(conversationHistory, openai.UserMessage(message))
		case "assistant":
			conversationHistory = append(conversationHistory, openai.AssistantMessage(message))
		}
	}
	if err := rows.Err(); err != nil {
		fmt.Printf("Error iterating rows: %v", err)
	}
	if err := tx.Commit(); err != nil {
		fmt.Printf("Error committing transaction: %v", err.Error())
	}
	return &conversationHistory
}

func LLMEntryPoint(db *sql.DB, user_input string, user_id uint64) *OverallState {
	_ = godotenv.Load()
	groq_key := os.Getenv("GROQ_API_KEY")
	client := openai.NewClient(
		option.WithAPIKey(groq_key),
		option.WithBaseURL("https://api.groq.com/openai/v1"),
	)
	ctx := context.Background()
	const n_past_messages uint8 = 10
	conversationHistory := ConversationHistory(db, user_id, n_past_messages)
	fmt.Printf("\n\nconversation history: %v\n\n", conversationHistory)
	ModelName := "moonshotai/kimi-k2-instruct-0905"

	Messages := []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(
				`You are a helpful assistant of a fitness app. You should inform the user that he can give the informations of:
				exercise name, sets, reps and weight. If the user message is passing workout information such as sets, reps, weight 
				you should output the keyword <WO> so the app system can flag the user-message to be processed. If the user asks about
				passed data from the database such as what is my max weight in Y exercise or any other query you should only answer
				<Q> so the app system will flag the user input as a query request. 
				IMPORTANT: Do NOT give to the user hint that there are flags that are used to classify messages.
				Obs: I will give you the 10 previous messages as context:`),
			
	}
	Messages = append(Messages, *conversationHistory...)
	Messages = append(Messages, openai.UserMessage(strings.ToLower(user_input)))

	chat, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: ModelName,
		Messages: Messages,
	})
	if err != nil {
		panic(err.Error())
	}
	chat_response_content := &chat.Choices[0].Message.Content
	return &OverallState{
		UserID: int(user_id),
		UserInput: user_input,
		Messages: []Message{
			Message{Role: "user", Content: user_input},
			Message{Role: "assistant", Content: *chat_response_content},
		},
	}
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
	defer rows.Close()
	if err != nil {
		fmt.Printf("LLM Error: %v", err.Error())
		return
	}
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

func LLMStructuredOutputSets(state *OverallState, db *sql.DB) {
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

func LLMRouteInput(state *OverallState, db *sql.DB) {
	if state.Messages[len(state.Messages)-1].Content == "<WO>" {
		LLMStructuredOutputSets(state, db)
	} 
	if state.Messages[len(state.Messages)-1].Content == "<Q>" {
		LLMQueryData(db, state)
	}
}

func createDatabase(dbName, initSQLPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dbName)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %v", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to connect to database: %v", err)
	}
	if err := executeInitSQL(db, initSQLPath); err != nil {
		return nil, err
	}
	fmt.Printf("Database '%s' initialized successfully\n", dbName)
	return db, nil
}

func executeInitSQL(db *sql.DB, initSQLPath string) error {
	if _, err := os.Stat(initSQLPath); os.IsNotExist(err) {
		return fmt.Errorf("init SQL file not found: %s", initSQLPath)
	}
	sqlBytes, err := os.ReadFile(initSQLPath)
	if err != nil {
		return fmt.Errorf("failed to read init SQL file: %v", err)
	}
	sqlContent := string(sqlBytes)
	statements := strings.Split(sqlContent, ";")
	for i, stmt := range statements {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		_, err := db.Exec(stmt)
		if err != nil {
			return fmt.Errorf("failed to execute statement %d: %v\nStatement: %s", i+1, err, stmt)
		}
	}
	fmt.Printf("Executed initialization script: %s\n", initSQLPath)
	return nil
}

func InsertOverallState(db *sql.DB, state *OverallState) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, msg := range state.Messages {
		_, err := tx.Exec(`
			INSERT INTO messages (user_id, role, message, created_at)
			VALUES (?, ?, ?, ?);
		`, state.UserID, msg.Role, msg.Content, time.Now())
		if err != nil {
			return fmt.Errorf("failed to insert message: %w", err)
		}
	}
	for _, ex := range state.ExerciseList.Exercises {
		exercise_name := ex.Exercise
		for _, set := range ex.ExerciseSets {
			_, err := tx.Exec(`
				INSERT INTO workout_sets (user_id, exercise, weight, reps, created_at)
				VALUES (?, ?, ?, ?, ?);
				`, state.UserID, exercise_name, set.Weight, set.NReps, time.Now())
			if err != nil {
				return fmt.Errorf("failed to insert workout: %w", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}
