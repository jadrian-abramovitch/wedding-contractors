package main

import (
	"fmt"
	"html/template"
	"net/http"
	"os"
)

type Page struct {
	Title string
	id    int
}

func handler(w http.ResponseWriter, r *http.Request) {

	page := Page{"hi", 1}
	t, _ := template.ParseFiles("edit.html")
	t.Execute(w, page)
}

func main() {
	fmt.Println("Hello!")

	http.HandleFunc("/", handler)
	fmt.Println("listening on http://localhost:")
	if err := http.ListenAndServe(":8086", nil); err != nil {
		fmt.Println("http.ListenAndServe():", err)
		os.Exit(1)
	}
}
