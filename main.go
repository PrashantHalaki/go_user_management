package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/go-playground/validator/v10"
	_ "github.com/go-sql-driver/mysql"
)

var validate *validator.Validate

func openDB() *sql.DB {
	db, error := sql.Open("mysql", os.Getenv("USER_MANAGEMENT_MYSQL_CONNECTION_URL"))
	if error != nil {
		panic(error)
	} else {
		fmt.Println("Connected to Database")
		return db
	}
}

var DB = openDB()

func createTableIfNotExist() {
	creation, creationError := DB.Exec(`CREATE TABLE IF NOT EXISTS users (
		id int(11) NOT NULL auto_increment PRIMARY KEY,
		first_name varchar(250) NOT NULL,
		last_name varchar(250),
		email varchar(250) NOT NULL,
		password varchar(250) NOT NULL,
		status int(2) NOT NULL DEFAULT 1,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
		deleted_at DATETIME
	)`)
	if creationError != nil {
		panic(creationError)
	} else {
		fmt.Println(creation)
	}
}

type UserAPI struct{}

type User struct {
	ID        int64        `json:"id"`
	FirstName string       `json:"first_name" validate:"required,alpha"`
	LastName  string       `json:"last_name" validate:"alpha"`
	Email     string       `json:"email" validate:"required,email"`
	Password  string       `json:"password,omitempty" validate:"required,min=8"`
	Status    int          `json:"status"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
	DeletedAt sql.NullTime `json:"-"`
}

func validateStruct(e User) error {
	validate = validator.New()
	err := validate.Struct(e)
	if err != nil {
		return err
	}
	return nil
}

var lock sync.Mutex

func (u *UserAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("method '%v' to %v\n", r.Method, r.URL)
	switch r.Method {
	case http.MethodGet:
		getUsers(w, r)
	case http.MethodPost:
		createUser(w, r)
	case http.MethodPut:
		updateUser(w, r)
	case http.MethodDelete:
		deleteUser(w, r)
	default:
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "Unsupported method '%v' to %v\n", r.Method, r.URL)
		log.Printf("Unsupported method '%v' to %v\n", r.Method, r.URL)
	}
}

func hashAndSalt(pwd string) string {

	// Use GenerateFromPassword to hash & salt pwd
	// MinCost is just an integer constant provided by the bcrypt
	// package along with DefaultCost & MaxCost.
	// The cost can be any value you want provided it isn't lower
	// than the MinCost (4)
	hash, err := bcrypt.GenerateFromPassword([]byte(pwd), bcrypt.MinCost)
	if err != nil {
		log.Println(err)
	}
	// GenerateFromPassword returns a byte slice so we need to
	// convert the bytes to a string and return it
	return string(hash)
}

func comparePasswords(hashedPwd string, plainPwd string) bool {
	// Since we'll be getting the hashed password from the DB it
	// will be a string so we'll need to convert it to a byte slice
	byteHash := []byte(hashedPwd)
	err := bcrypt.CompareHashAndPassword(byteHash, []byte(plainPwd))
	if err != nil {
		log.Println(err)
		return false
	}

	return true
}

func main() {
	var PORT string
	if os.Getenv("PORT") != "" {
		PORT = os.Getenv("PORT")
	} else {
		PORT = "8080"
	}
	createTableIfNotExist()
	salt := hashAndSalt("This is passs")
	fmt.Println(salt, comparePasswords(salt, "This is pass"))
	fmt.Println("Application is running on http://localhost:" + PORT)
	http.HandleFunc("/", welcomeNote)
	http.Handle("/users", &UserAPI{})
	http.ListenAndServe(":"+PORT, nil)
	err := http.ListenAndServeTLS(":443", "server.crt", "server.key", nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
	defer DB.Close()
}
func welcomeNote(w http.ResponseWriter, req *http.Request) {
	w.Write([]byte("Welcome to the site."))
}
func (u User) MarshalJSON() ([]byte, error) {
	type user User // prevent recursion
	x := user(u)
	x.Password = ""
	return json.Marshal(x)
}
func getUsers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	userList, err := DB.Query("select * from users;")

	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
	}
	var response = []User{}
	for userList.Next() {
		var userObj User
		// for each row, scan the result into our tag composite object
		err = userList.Scan(&userObj.ID, &userObj.FirstName, &userObj.LastName, &userObj.Email, &userObj.Password, &userObj.Status, &userObj.CreatedAt, &userObj.UpdatedAt, &userObj.DeletedAt)
		if err != nil {
			http.Error(w, err.Error(), 400) // proper error handling instead of panic in your app
		}

		response = append(response, userObj)
	}
	json.NewEncoder(w).Encode(response)
}

func createUser(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var userObj User
	err := json.NewDecoder(r.Body).Decode(&userObj)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	structErr := validateStruct(userObj)
	if structErr != nil {
		http.Error(w, structErr.Error(), http.StatusBadRequest)
		return
	}
	// // start of protected code changes
	lock.Lock()
	var count int
	checkUser, userCheckErr := DB.Query("select count(*) from users where email='" + userObj.Email + "';")
	if userCheckErr != nil {
		http.Error(w, userCheckErr.Error(), http.StatusBadRequest)
		return
	}
	checkUser.Next()
	checkUser.Scan(&count)
	if count > 0 {
		http.Error(w, "Email already exist.", 400)
		return
	}
	password := hashAndSalt(userObj.Password)
	createdUser, creationErr := DB.Exec(`insert into users(first_name,last_name,email,password) values("` + userObj.FirstName + `","` + userObj.LastName + `","` + userObj.Email + `","` + password + `");`)
	// end protected code changes
	if creationErr != nil {
		fmt.Println("creaeeerrrr")
		http.Error(w, creationErr.Error(), 400)
		return
	}
	insertedId, insertError := createdUser.LastInsertId()
	if insertError != nil {
		http.Error(w, insertError.Error(), 400)
		return
	}
	userObj.ID = insertedId
	userObj.CreatedAt = time.Now()
	userObj.UpdatedAt = time.Now()
	userObj.Status = 1
	lock.Unlock()
	json.NewEncoder(w).Encode(userObj)
}

func updateUser(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	var userId = r.URL.Query().Get("userId")
	if userId == "" {
		http.Error(w, "userId is missing in query!", 400)
		return
	}
	jd := json.NewDecoder(r.Body)

	aUser := &User{}
	err := jd.Decode(aUser)
	if nil != err {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// start of protected code changes
	lock.Lock()
	updateUser, updateError := DB.Exec("update users set first_name = ?, last_name =  ? where id = ? ;", aUser.FirstName, aUser.LastName, userId)
	if updateError != nil {
		http.Error(w, updateError.Error(), 400)
		return
	}
	updatedRows, updtErr := updateUser.RowsAffected()
	if updtErr != nil {
		http.Error(w, updtErr.Error(), 400)
		return
	}
	// end protected code changes
	lock.Unlock()
	fmt.Fprintf(w, "Successfully updated %d row.", updatedRows)
}

func deleteUser(w http.ResponseWriter, r *http.Request) {

	// get the user ID from the path
	// fields := strings.Split(r.URL.String(), "/")
	// id, err := strconv.ParseUint(fields[len(fields)-1], 10, 64)
	var id = r.URL.Query().Get("userId")
	if id == "" {
		http.Error(w, "userId is missing in query!", 400)
		return
	}
	// if nil != err {
	// 	http.Error(w, err.Error(), http.StatusBadRequest)
	// 	return
	// }

	log.Printf("Request to delete user %v", id)

	// start of protected code changes
	lock.Lock()
	_, er := DB.Exec("update users set status = ? , deleted_at = ? where id = ? ;", -1, time.Now(), id)
	if er != nil {
		http.Error(w, er.Error(), 400)
		return
	}
	// end protected code changes
	lock.Unlock()
	fmt.Fprintf(w, "Deleted user successfully!")
}
