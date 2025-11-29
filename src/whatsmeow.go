package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"strconv"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"

	"github.com/joho/godotenv"
	_ "github.com/mattn/go-sqlite3"
)

func eventHandler(client *whatsmeow.Client, db *sql.DB) func(any) {
	return func(evt any) {
		switch v := evt.(type) {
		case *events.Message:
			var message string

			// Try plain conversation text
			if v.Message.GetConversation() != "" {
				message = v.Message.GetConversation()
			}

			// Try extended text message
			if v.Message.ExtendedTextMessage != nil &&
				v.Message.ExtendedTextMessage.Text != nil {
				message = *v.Message.ExtendedTextMessage.Text
			}

			// (Optional) handle other message types, e.g. images with captions:
			if v.Message.ImageMessage != nil &&
				v.Message.ImageMessage.Caption != nil {
				message = *v.Message.ImageMessage.Caption
			}

			sender := v.Info.Sender

			device := sender.User
			deviceID, err := strconv.ParseUint(device, 10, 64)
			fmt.Printf("\n id %v\n", deviceID)
			if err != nil {
				fmt.Printf("Error converting phone number: %v\n", err)
				return
			}

			fmt.Println("Received a message!\n", message)
			fmt.Println("Device is: \n", device)

			state := LLMMessageClassifier(deviceID, message)
			LLMRouteInput(db, state)

			tx, err := db.Begin()
			if err != nil {
				fmt.Printf("Error to connect to db: %v", err.Error())
				return
			}
			defer tx.Rollback()

			for _, msg := range state.Messages {
					_, err := tx.Exec(`
					INSERT INTO messages (user_id, role, message)
					VALUES (?, ?, ?);`,
					state.UserID, msg.Role, msg.Content, time.Now())
				if err != nil {
					fmt.Printf("failed to insert message: %v", err.Error())
					return
					}
				}
			if err := tx.Commit(); err != nil {
				fmt.Printf("failed to commit transaction: %v", err.Error())
    		return
			}
			response := state.Messages[len(state.Messages)-1].Content
			if message != "" {
				userJID := types.JID{
					User:   sender.User,
					Server: sender.Server,
				}
				_, err := client.SendMessage(
					context.Background(),
					userJID,
					&waE2E.Message{Conversation: &response},
				)

				if err != nil {
					fmt.Printf("Error sending message: %v\n", err)
				} else {
					fmt.Println("Response sent successfully")
				}
			}
		}
	}
}

func main() {

	db, err := createDatabase("fitness_app.db", "init.sql")
	if err != nil {
		fmt.Printf("Failed to initialize database: %v", err)
		return
	}
  defer db.Close()
	
	err = godotenv.Load()
	if err != nil {
		log.Fatalf("Error loading .env file")
	}

	dbPath := os.Getenv("DB_PATH")
	dsn := fmt.Sprintf("file:%s?_foreign_keys=on", dbPath)
	dbLog := waLog.Stdout("Database", "DEBUG", true)
	ctx := context.Background()
	container, err := sqlstore.New(ctx, "sqlite3", dsn, dbLog)
	if err != nil {
		panic(err)
	}
	// If you want multiple sessions, remember their JIDs and use .GetDevice(jid) or .GetAllDevices() instead.
	deviceStore, err := container.GetFirstDevice(ctx)
	if err != nil {
		panic(err)
	}
	clientLog := waLog.Stdout("Client", "DEBUG", true)
	client := whatsmeow.NewClient(deviceStore, clientLog)
	client.AddEventHandler(eventHandler(client, db))

	if client.Store.ID == nil {
		// No ID stored, new login
		qrChan, _ := client.GetQRChannel(context.Background())
		err = client.Connect()
		if err != nil {
			panic(err)
		}
		for evt := range qrChan {
			if evt.Event == "code" {
				// Render the QR code here
				// e.g. qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
				// or just manually `echo 2@... | qrencode -t ansiutf8` in a terminal
				fmt.Println("QR code:", evt.Code)
			} else {
				fmt.Println("Login event:", evt.Event)
			}
		}
	} else {
		// Already logged in, just connect
		err = client.Connect()
		if err != nil {
			panic(err)
		}
	}

	// Listen to Ctrl+C (you can also do something else that prevents the program from exiting)
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	client.Disconnect()
}
