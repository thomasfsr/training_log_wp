package llm
type extractTask struct {
	LabelTask       string `json:"label_task" jsonschema:"enum=query_data,enum=update,enum=chat" jsonschema_description:"The label of the task if it is quering database, updating the database or neither just chatting."`
	TaskDescription string `json:"task_description" jsonschema_description:"The task with the main informations to execute the user request"`
}

type ListOfTasks struct {
	Tasks []extractTask `json:"tasks" jsonschema_description:"List of tasks extracted from the user input."`
}

type ListOfExercises struct {
	Exercises []ExerciseData `json:"exercises" jsonschema_description:"List of exercises, each exercise with its on sets."`
}

type ExerciseData struct {
	Exercise     string        `json:"exercise" jsonschema_description:"Exercise name"`
	ExerciseSets []ExerciseSet `json:"exercise_sets" jsonschema_description:"the sets of the exercise."`
}

type ExerciseSet struct {
	NReps  uint8   `json:"n_reps" jsonschema_description:"number of reps of the exercise set"`
	Weight float32 `json:"weight" jsonschema_description:"weight of the exercise set in kilograms (kg)"`
}

type Category struct { 
	Category string `json:"category" jsonschema:"enum=insert,enum=query,enum=chat" jsonschema_description:"Classification of the user input to: insert, query or chat."`
}

type Message struct {
	Role string
	Content string
}

type OverallState struct {
	UserID     uint64
	UserInput    string
	Category string
	// SQL string
	Messages     []Message
	ExerciseList *ListOfExercises
}
