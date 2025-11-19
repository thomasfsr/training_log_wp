package main

import (
	"fmt"
	"os"
	"database/sql"
	"strings"

	"github.com/invopop/jsonschema"
	"github.com/openai/openai-go/v2"
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
var CategorySchema = GenerateSchema[Category]()

func cleanSQLResponse(sql string) string {
	cleaned := strings.TrimPrefix(sql, "```sql")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)
	return cleaned
}

func InsertOverallState(db *sql.DB, state *OverallState) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, ex := range state.ExerciseList.Exercises {
		exercise_name := ex.Exercise
		for _, set := range ex.ExerciseSets {
			_, err := tx.Exec(`
				INSERT INTO workout_sets (user_id, exercise, weight, reps)
				VALUES (?, ?, ?, ?);
				`, state.UserID, exercise_name, set.Weight, set.NReps)
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
