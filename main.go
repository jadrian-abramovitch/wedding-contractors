package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"text/template"
	"wedding-contractors/auth"

	"github.com/gorilla/pat"
	"github.com/jackc/pgx/v5"
	"github.com/markbates/goth/gothic"
)

type Page struct {
	Title string
	id    int
}

type User struct {
	userId    int    `db:userId`
	firstName string `db: firstName`
	lastName  string `db: lastName`
	email     string `db: email`
}

func handler(w http.ResponseWriter, r *http.Request) {

	page := Page{"HMR?", 1}
	t, _ := template.ParseFiles("edit.html")
	t.Execute(w, page)
}

func bootstrapData(conn *pgx.Conn) error {

	_, err := conn.Exec(context.Background(), "CREATE TABLE IF NOT EXISTS users (userId BIGINT, firstName TEXT, lastName TEXT, email TEXT)")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to create tables: %v\n", err)
		return err
	}

	_, err = conn.Exec(context.Background(), "INSERT INTO users (userId, firstName, lastName, email) VALUES (1, 'Lauren', 'Homann', 'test@gmail.com')")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to insert users: %v\n", err)
		return err
	}
	return nil
}

func main() {

	auth.NewAuth()

	m := map[string]string{
		"google": "Google",
	}
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	providerIndex := &ProviderIndex{Providers: keys, ProvidersMap: m}
	conn, err := pgx.Connect(context.Background(), os.Getenv("DATABASE_URL"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to connect to database: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close(context.Background())

	err = bootstrapData(conn)
	if err != nil {
		os.Exit(1)
	}

	p := pat.New()
	p.Get("/users", func(w http.ResponseWriter, r *http.Request) {
		// var email string
		var user User
		err := conn.QueryRow(context.Background(), "SELECT email FROM users where userId=$1", 1).Scan(&user.email)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error selecting users: %v\n", err)
		}
		t, _ := template.ParseFiles("users.html")
		fmt.Println(user)
		t.Execute(w, user)
	})

	p.Get("/auth/{provider}/callback", func(res http.ResponseWriter, req *http.Request) {

		user, err := gothic.CompleteUserAuth(res, req)
		if err != nil {
			fmt.Fprintln(res, err)
			return
		}
		t, _ := template.New("foo").Parse(userTemplate)
		t.Execute(res, user)
	})

	p.Get("/logout/{provider}", func(res http.ResponseWriter, req *http.Request) {
		gothic.Logout(res, req)
		res.Header().Set("Location", "/")
		res.WriteHeader(http.StatusTemporaryRedirect)
	})

	p.Get("/auth/{provider}", func(res http.ResponseWriter, req *http.Request) {
		// try to get the user without re-authenticating
		if gothUser, err := gothic.CompleteUserAuth(res, req); err == nil {
			t, _ := template.New("foo").Parse(userTemplate)
			t.Execute(res, gothUser)
		} else {
			gothic.BeginAuthHandler(res, req)
		}
	})

	p.Get("/", func(res http.ResponseWriter, req *http.Request) {
		t, _ := template.New("foo").Parse(indexTemplate)
		t.Execute(res, providerIndex)
	})

	fmt.Println("listening on http://localhost:")
	log.Println("listening on localhost:8000")
	log.Fatal(http.ListenAndServe(os.Getenv("EXPOSED_PORT"), p))
}

type ProviderIndex struct {
	Providers    []string
	ProvidersMap map[string]string
}

var indexTemplate = `{{range $key,$value:=.Providers}}
    <p><a href="/auth/{{$value}}">Log in with {{index $.ProvidersMap $value}}</a></p>
{{end}}`

var userTemplate = `
<p><a href="/logout/{{.Provider}}">logout</a></p>
<p>Name: {{.Name}} [{{.LastName}}, {{.FirstName}}]</p>
<p>Email: {{.Email}}</p>
<p>NickName: {{.NickName}}</p>
<p>Location: {{.Location}}</p>
<p>AvatarURL: {{.AvatarURL}} <img src="{{.AvatarURL}}"></p>
<p>Description: {{.Description}}</p>
<p>UserID: {{.UserID}}</p>
<p>AccessToken: {{.AccessToken}}</p>
<p>ExpiresAt: {{.ExpiresAt}}</p>
<p>RefreshToken: {{.RefreshToken}}</p>
`
