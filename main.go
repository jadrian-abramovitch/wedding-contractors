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

type User struct {
	UserId      int
	FirstName   string
	LastName    string
	AuthService string
	IdToken     string
	ExpiresAt   time.Time
}

type AuthContext struct {
	store *sessions.CookieStore
	db    *pgx.Conn
}

type ContextKey string

type ProviderIndex struct {
	Providers    []string
	ProvidersMap map[string]string
}

var ctxKey ContextKey = "user"

func bootstrapData(conn *pgx.Conn) error {

	_, err := conn.Exec(context.Background(), "DROP TABLE IF EXISTS users")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to create tables: %v\n", err)
		return err
	}
	_, err = conn.Exec(context.Background(), "CREATE TABLE IF NOT EXISTS users (userId integer primary key generated always as identity, firstName TEXT, lastName TEXT, authService TEXT, idToken TEXT, expiresAt TIMESTAMP NOT NULL)")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to create tables: %v\n", err)
		return err
	}

	// _, err = conn.Exec(context.Background(), "INSERT INTO users (firstName, lastName, authService, idToken, ) VALUES ('Lauren', 'Homann', 'google', '1')")
	// if err != nil {
	// 	fmt.Fprintf(os.Stderr, "Unable to insert users: %v\n", err)
	// 	return err
	// }
	return nil
}

func (bg *AuthContext) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("hello from handler func")
		if strings.HasPrefix(r.URL.Path, "/auth") || strings.HasPrefix(r.URL.Path, "/users") {
			next.ServeHTTP(w, r)
			return
		}
		session, err := bg.store.Get(r, os.Getenv("SESSION_NAME"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if session.Values["idToken"] == nil || session.Values["userId"] == nil {
			http.Redirect(w, r, "/auth/login", http.StatusTemporaryRedirect)
			return
		}
		incomingId, ok := session.Values["userId"].(int)
		if !ok {
			fmt.Println("couldn't parse user id")
			http.Redirect(w, r, "/auth/login", http.StatusTemporaryRedirect)
			return
		}

		var user User
		bg.db.QueryRow(context.TODO(), "SELECT * FROM users WHERE userId = $1", incomingId).Scan(&user.UserId, &user.FirstName, &user.LastName, &user.AuthService, &user.IdToken, &user.ExpiresAt)
		if user.IdToken == "" || user.IdToken != session.Values["idToken"] {
			fmt.Println("user must log in 2")
			http.Redirect(w, r, "/auth/login", http.StatusTemporaryRedirect)
			return
		}

		if time.Now().Compare(user.ExpiresAt) == 1 {
			fmt.Println("Session expired")
			http.Redirect(w, r, "/auth/login", http.StatusTemporaryRedirect)
		}

		r = r.WithContext(context.WithValue(r.Context(), ctxKey, user))
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

	// HMR is causing this to re-run a whole bunch. Need to decouple bootstrap from main
	// err = bootstrapData(conn)
	// if err != nil {
	// 	os.Exit(1)
	// }

	p := pat.New()
	var store = sessions.NewCookieStore([]byte(os.Getenv("SESSION_KEY")))
	amw := AuthContext{store: store, db: conn}
	p.Use(amw.AuthMiddleware)
	p.Get("/users", func(w http.ResponseWriter, r *http.Request) {
		var numUsers int
		err = conn.QueryRow(context.TODO(), "SELECT COUNT(*) FROM users").Scan(&numUsers)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error selecting users: %v\n", err)
		}
		t, _ := template.ParseFiles("users.html")
		fmt.Println(numUsers)
		t.Execute(w, numUsers)
	})

	p.Get("/auth/{provider}/callback", func(res http.ResponseWriter, req *http.Request) {

		user, err := gothic.CompleteUserAuth(res, req)
		if err != nil {
			fmt.Fprintln(res, err)
			return
		}

		session, err := store.Get(req, os.Getenv("SESSION_NAME"))
		if err != nil {
			http.Error(res, err.Error(), http.StatusInternalServerError)
			return
		}

		if !time.Now().Before(user.ExpiresAt) {
			fmt.Println("token is not expired yet")
			return
		}

		var id int
		row := conn.QueryRow(context.TODO(), "SELECT userId FROM users WHERE idToken=$1", user.UserID)
		err = row.Scan(&id)
		if err != nil {
			// user does not exist in the system yet
			row = conn.QueryRow(context.TODO(), "INSERT INTO users (firstName, lastName, authService, idToken, expiresAt) VALUES ($1, $2, $3, $4, $5) RETURNING userId", user.FirstName, user.LastName, user.Provider, user.UserID, user.ExpiresAt)
		} else {
			// user exists in the system already
			_, updateErr := conn.Exec(context.TODO(), "UPDATE users SET expiresAt=$1 WHERE userId=$2", user.ExpiresAt, id)
			if updateErr != nil {
				fmt.Println("could not update user expires at")
			}
		}
		err = row.Scan(&id)
		if err != nil {
			fmt.Printf("error insert users: %s\n", err)
		}

		session.Values["idToken"] = user.UserID
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
		session, err := store.Get(req, os.Getenv("SESSION_NAME"))
		if err != nil {
			http.Error(res, err.Error(), http.StatusInternalServerError)
			return
		}

		gothic.Logout(res, req)
		session.Values["idToken"] = ""
		session.Values["userId"] = -1
		session.Save(req, res)

		res.Header().Set("Location", "/auth/login")
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
		user := req.Context().Value(ctxKey).(User)

		t, _ := template.New("home").Parse(homeTemplate)
		t.Execute(res, user)
	})

	log.Printf("listening on http://localhost%s", os.Getenv("EXPOSED_PORT"))
	log.Fatal(http.ListenAndServe(os.Getenv("EXPOSED_PORT"), p))
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
var homeTemplate = `<h1>Home</h1>
<p><a href="/logout/{{.AuthService}}">logout</a></p>
`
