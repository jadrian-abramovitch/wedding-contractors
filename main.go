package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"text/template"

	"github.com/jackc/pgx/v5"
)

type Page struct {
	Title string
	id    int
}

type User struct {
	userId    int
	firstName string
	lastName  string
	email     string
}

func handler(w http.ResponseWriter, r *http.Request) {

	page := Page{"HMR?", 1}
	t, _ := template.ParseFiles("edit.html")
	t.Execute(w, page)
}

func main() {

	conn, err := pgx.Connect(context.Background(), os.Getenv("DATABASE_URL"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to connect to database: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close(context.Background())

	_, err = conn.Exec(context.Background(), "CREATE TABLE IF NOT EXISTS users (userId BIGINT, firstName TEXT, lastName TEXT, email TEXT)")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to create tables: %v\n", err)
		os.Exit(1)
	}

	_, err = conn.Exec(context.Background(), "INSERT INTO users (userId, firstName, lastName, email) VALUES (1, 'Lauren', 'Homann', 'test@gmail.com')")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to insert users: %v\n", err)
		os.Exit(1)
	}

	var greeting string
	err = conn.QueryRow(context.Background(), "select 'Hello, world!!!'").Scan(&greeting)
	if err != nil {
		fmt.Fprintf(os.Stderr, "QueryRow failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(greeting)

	http.HandleFunc("/", handler)
	http.HandleFunc("/users", func(w http.ResponseWriter, r *http.Request) {
		var email string
		err := conn.QueryRow(context.Background(), "SELECT email FROM users where userId=$1", 1).Scan(&email)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error selecting users: %v\n", err)
		}
		t, _ := template.ParseFiles("users.html")
		fmt.Println(email)
		t.Execute(w, email)
	})
	fmt.Println("listening on http://localhost:")
	if err := http.ListenAndServe(os.Getenv("EXPOSED_PORT"), nil); err != nil {
		fmt.Println("http.ListenAndServe():", err)
		os.Exit(1)
	}
}
