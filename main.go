package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"text/template"
	"time"
	"wedding-contractors/auth"

	"github.com/gorilla/pat"
	"github.com/gorilla/sessions"
	"github.com/jackc/pgx/v5"
	"github.com/markbates/goth/gothic"
)

type Page struct {
	Title string
	id    int
}

type User struct {
	userId      int    `db:userId`
	firstName   string `db: firstName`
	lastName    string `db: lastName`
	authService string `db: authService`
	idToken     string `db: idToken`
}

type CustomStore struct {
	cookies *sessions.CookieStore
	db      *pgx.Conn
}

func handler(w http.ResponseWriter, r *http.Request) {

	page := Page{"HMR?", 1}
	t, _ := template.ParseFiles("edit.html")
	t.Execute(w, page)
}

func bootstrapData(conn *pgx.Conn) error {

	_, err := conn.Exec(context.Background(), "DROP TABLE IF EXISTS users")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to create tables: %v\n", err)
		return err
	}
	_, err = conn.Exec(context.Background(), "CREATE TABLE IF NOT EXISTS users (userId integer primary key generated always as identity, firstName TEXT, lastName TEXT, authService TEXT, idToken TEXT)")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to create tables: %v\n", err)
		return err
	}

	_, err = conn.Exec(context.Background(), "INSERT INTO users (firstName, lastName, authService, idToken) VALUES ('Lauren', 'Homann', 'google', '1')")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to insert users: %v\n", err)
		return err
	}
	return nil
}

func (store *CustomStore) AuthMiddleware(next http.Handler) http.Handler {
	cookieStore := store.cookies
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/auth") || strings.HasPrefix(r.URL.Path, "/users") {
			fmt.Println("skip middle ware")
			next.ServeHTTP(w, r)
			return
		}
		session, err := cookieStore.Get(r, "session-name")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if session.Values["idToken"] == nil {
			fmt.Println("user must log in")
			http.Redirect(w, r, "/auth/login", http.StatusTemporaryRedirect)
			return
		}
		if session.Values["userId"] == nil {
			fmt.Println("user has no id")
			http.Redirect(w, r, "/auth/login", http.StatusTemporaryRedirect)
			return
		}
		incomingId, ok := session.Values["userId"].(int)
		if !ok {
			fmt.Println("couldn't make user id an int")
		}
		fmt.Printf("userId is: %d\n", session.Values["userId"])
		// check that the idToken is actually valid
		// no way that is actually properly validated and escaped, right?????
		row := store.db.QueryRow(context.Background(), "SELECT idToken FROM users WHERE userId=$1", incomingId)
		// TODO check err
		// if err != nil {
		// 	fmt.Println("Query to get tokens didn't work")
		// }
		matched := false
		var dbToken string
		row.Scan(dbToken)
		fmt.Printf("db token: %s, incoming: %s", dbToken, session.Values["idToken"])
		if dbToken == session.Values["idToken"] {
			matched = true
		}
		// this does not scale well at all, need to figure out how to embed userId into cookie
		// for rows.Next() {
		// 	rows.Scan(&dbToken)
		// 	fmt.Printf("dbToken: %s\n", dbToken)
		// 	if dbToken == session.Values["idToken"] {
		// 		matched = true
		// 	}
		// }
		if !matched {
			fmt.Println("user must log in 2")
			http.Redirect(w, r, "/auth/login", http.StatusTemporaryRedirect)
			return
		}
		fmt.Printf("session: %s\n", session.Values["idToken"])
		fmt.Println("hello from middle ware")
		next.ServeHTTP(w, r)
	})
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
	var store = sessions.NewCookieStore([]byte(os.Getenv("SESSION_KEY")))
	amw := CustomStore{cookies: store, db: conn}
	p.Use(amw.AuthMiddleware)
	p.Get("/users", func(w http.ResponseWriter, r *http.Request) {
		session, err := store.Get(r, "session-name")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		idToken := session.Values["idToken"]
		fmt.Printf("idToken: %s\n", idToken)
		var email string
		// var user User
		err = conn.QueryRow(context.Background(), "SELECT idToken FROM users where userId=$1", 2).Scan(&email)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error selecting users: %v\n", err)
		}
		t, _ := template.ParseFiles("users.html")
		fmt.Println(email)
		t.Execute(w, email)
	})

	p.Get("/cookies", func(res http.ResponseWriter, req *http.Request) {
		// Get a session. Get() always returns a session, even if empty.
		session, err := store.Get(req, "session-name")
		if err != nil {
			http.Error(res, err.Error(), http.StatusInternalServerError)
			return
		}

		// Set some session values.
		session.Values["foo"] = "bar"
		session.Values[42] = 43
		// Save it before we write to the response/return from the handler.
		err = session.Save(req, res)
		if err != nil {
			http.Error(res, err.Error(), http.StatusInternalServerError)
			return
		}
	})

	p.Get("/auth/{provider}/callback", func(res http.ResponseWriter, req *http.Request) {

		user, err := gothic.CompleteUserAuth(res, req)
		if err != nil {
			fmt.Fprintln(res, err)
			return
		}

		session, err := store.Get(req, "session-name")
		if err != nil {
			http.Error(res, err.Error(), http.StatusInternalServerError)
			return
		}

		if !time.Now().Before(user.ExpiresAt) {
			fmt.Println("token is not expired yet")
			return
		}
		var id int
		// this shouldn't always insert, should update if user existed before but not logged in
		row := conn.QueryRow(context.Background(), "INSERT INTO users (firstName, lastName, authService, idToken) VALUES ($1, $2, $3, $4) RETURNING userId", user.FirstName, user.LastName, "google", user.IDToken)
		// check error TODO
		row.Scan(&id)
		fmt.Printf("id: %d\n", id)
		// if err != nil {
		// 	fmt.Fprintln(res, err)
		// 	return
		// }

		session.Values["idToken"] = user.IDToken
		session.Values["userId"] = id
		err = session.Save(req, res)
		if err != nil {
			http.Error(res, err.Error(), http.StatusInternalServerError)
			return
		}
		t, _ := template.New("foo").Parse(userTemplate)
		t.Execute(res, user)
	})

	p.Get("/logout/{provider}", func(res http.ResponseWriter, req *http.Request) {
		gothic.Logout(res, req)
		fmt.Printf("url user: %s\n", req.URL.User)
		// _, err = conn.Exec(context.Background(), "DELETE FROM users WHERE ")
		res.Header().Set("Location", "/")
		res.WriteHeader(http.StatusTemporaryRedirect)
	})

	p.Get("/auth/login", func(res http.ResponseWriter, req *http.Request) {
		fmt.Println("test from /auth/login")
		t, _ := template.New("foo").Parse(indexTemplate)
		t.Execute(res, providerIndex)
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
		t, _ := template.New("home").Parse(homeTemplate)
		t.Execute(res, 1)
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
var homeTemplate = `<h1>Home</h1>`
