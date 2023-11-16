package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

type (
	Agent struct {
		addr string
		dbusername string
		dbpassword string
	}
	RunCodeRequest struct {
		ID string `json:"id"`
		Code string `json:"code"`
		Database string `json:"database"`
		Raw bool `json:"raw"`
	}
	apiFunc func(http.ResponseWriter, *http.Request) error
	APIRawResponse struct {
		Message      string `json:"message"`
		Error        string `json:"error"`
		Stdout       string `json:"stdout"`
		Stderr       string `json:"stderr"`
		ExecDuration int64  `json:"exec_duration"`
		MemUsage     int64  `json:"mem_usage"`
	}
	APIClientResponse struct {
		Message      string `json:"message"`
		Columns 	 []string `json:"columns"`
		Types		 []*sql.ColumnType
		Results      []map[string]interface{} `json:"results"`
		ExecDuration int64  `json:"exec_duration"`
		MemUsage     int64  `json:"mem_usage"`

	}
)

func main() {
	a := &Agent{
		addr: ":8080",
		dbusername: "root",
		dbpassword: "root",
	}
	a.init()
}

// --- agent start ---

func (a *Agent) init(){
	log.Println("Starting agent")
	mux := http.NewServeMux()

	mux.HandleFunc("/run", a.makeHTTPHandlerFunc(a.handleRunCode))
	mux.HandleFunc("/health", a.makeHTTPHandlerFunc(a.handleHealth));

	log.Println("Agent started on port ", a.addr)
	http.ListenAndServe(a.addr, mux)
	
}

func (a *Agent)makeHTTPHandlerFunc(f apiFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := f(w, r)
		if err != nil {
			WriteJSON(w,http.StatusInternalServerError,APIRawResponse{ Error: err.Error()})
		}
	}
}

func (a *Agent) handleRunCode(w http.ResponseWriter, r *http.Request) error {
	err := supportedMethod(r.Method, "POST"); if err != nil {
		return err
	}

	var req RunCodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return err
	}

	if(req.Code == "" || req.ID=="" || req.Database=="") {
		WriteJSON(w,http.StatusInternalServerError,APIRawResponse{ Error: "code , id and database are required"})
		return nil
	}

	filename := "./tmp/" + req.ID;
	log.Printf("using temp file %s", filename)

	if req.Raw {
		return a.runRawCode(w, &req)
	}
	

	return a.runUsingDriver(w, &req)
}
func (a *Agent) handleHealth(w http.ResponseWriter, r *http.Request) error {
	err := supportedMethod(r.Method, "GET"); if err != nil {
		return err
	}
	w.Write([]byte("OK"))
	return nil
}

type ARow struct{
	ID        int    `json:"id"`
	Name string `json:"name"`
}

func (a *Agent) runUsingDriver(w http.ResponseWriter, req *RunCodeRequest) error {
	log.Printf("opening db connection")
	db, err := sql.Open("mysql", a.dbusername+":"+a.dbpassword+"@tcp(127.0.0.1:3306)/"+req.Database); if err !=nil {
		return err
	}
	defer db.Close()
	err = db.Ping(); if err != nil {
		return err
	}
	start := time.Now()
	rows, err := db.Query(req.Code); if err != nil {
		a.badRequest(w, err)
		return nil
	}
	defer rows.Close()
	elapsed := time.Since(start)

	columns, err := rows.Columns(); if err != nil {
		a.badRequest(w, err)
		return nil
	}

	columnTypes, err := rows.ColumnTypes(); if err != nil {
		a.badRequest(w, err)
		return nil
	}

	// columnTypes.Each(func(ct *sql.ColumnType) error {
	// 	ct.ScanType()
	// })
	log.Println("column types", columnTypes)

	numCols := len(columns)
	log.Println("numCols", numCols)
	var results []map[string]interface{}
	// if(numCols > 0) {
	// 	for rows.Next() {
	// 		values := make([]interface{}, numCols)
	// 		valuePointers := make([]interface{}, numCols)
	// 		for i := range values {
	// 			valuePointers[i] = &values[i]
	// 		}
	// 		err := rows.Scan(valuePointers...)
	// 		if err != nil {
	// 			a.badRequest(w, err)
	// 			return nil
	// 		}
	// 		row := make(map[string]interface{})
	// 		for i, column := range columns {
	// 			val := valuePointers[i].(*interface{})
	// 			row[column] = *val
	// 		}
	// 		log.Println("row", row)
	// 		results = append(results, row)
	// 	}
	// }

	// create a fieldbinding object.
	fb := NewFieldBinding()

	fb.PutFields(columns)

	//
	outArr := []interface{}{}

	for rows.Next() {
		if err := rows.Scan(fb.GetFieldPtrArr()...); err != nil {
			a.badRequest(w, err)
			return nil
		}

		fmt.Printf("Row: %v", fb.Get("id"))
		outArr = append(outArr, fb.GetFieldArr())
	}

	if err := rows.Err(); err != nil {
		a.badRequest(w, err)
        return nil
    }
	
	WriteJSON(w, http.StatusOK, APIClientResponse{
			Message: "Success",
			Columns: columns,
			Types: columnTypes,
			Results: results,
			ExecDuration: elapsed.Milliseconds(),
	})
	
	return nil;
}

func (a *Agent) runRawCode(w http.ResponseWriter, req *RunCodeRequest) error {
	log.Printf("create file ./tmp/%s", req.ID)
	err := os.Mkdir("./tmp", 0777); if err != nil {
		log.Printf("Dir ./tmp already exists %s", err.Error())
	}
	f, err := os.Create("./tmp/" + req.ID)
	if err != nil {
		log.Printf("Failed to create  file ./tmp/%s", req.ID)
		return err
	}

	defer f.Close()

	log.Printf("write code to file ./tmp/%s", req.ID)
	_, err = f.WriteString(req.Code); if err != nil {
		log.Printf("Failed to write to file ./tmp/%s", req.ID)
		return err	
	}

	start := time.Now()
	var compileStdOut, compileStdErr bytes.Buffer
	compileCmd := exec.Command("mysql", req.Database, "<", "./tmp/"+req.ID)
	compileCmd.Stdout = &compileStdOut
	compileCmd.Stderr = &compileStdErr
	err = compileCmd.Run()
	elapsed := time.Since(start)

	if err != nil {
		WriteJSON(w, http.StatusBadRequest, APIRawResponse{
			Message: "Failed to compile code",
			Error: err.Error(),
			Stdout: compileStdOut.String(),
			Stderr: compileStdErr.String(),
			ExecDuration: elapsed.Milliseconds(),
		})
		return nil
	}

	WriteJSON(w, http.StatusOK, APIRawResponse{
		Message: "Successfully compiled code",
		Stdout: compileStdOut.String(),
		Stderr: compileStdErr.String(),
		ExecDuration: elapsed.Milliseconds(),
	})
	return nil
}

func (a *Agent) badRequest(w http.ResponseWriter, err error) {
	WriteJSON(w, http.StatusBadRequest, APIRawResponse{
		Error: err.Error(),
	})
}
// --- agent end ---



// ------ end language

// --- untils start ---
func WriteJSON(w http.ResponseWriter, status int, v any) error {
	w.Header().Add("Content-Type", "application/json");
	w.WriteHeader(status);
	return json.NewEncoder(w).Encode(v)
}

func supportedMethod(m string, s string) error {
	if m != s {
		return errors.New("method not supported")
	}
	return nil
}
// --- untils end ---


// NewFieldBinding ...
func NewFieldBinding() *FieldBinding {
	return &FieldBinding{}
}

// FieldBinding is deisgned for SQL rows.Scan() query.
type FieldBinding struct {
	sync.RWMutex // embedded.  see http://golang.org/ref/spec#Struct_types
	FieldArr     []interface{}
	FieldPtrArr  []interface{}
	FieldCount   int64
	MapFieldToID map[string]int64
}

func (fb *FieldBinding) put(k string, v int64) {
	fb.Lock()
	defer fb.Unlock()
	fb.MapFieldToID[k] = v
}

// Get ...
func (fb *FieldBinding) Get(k string) interface{} {
	fb.RLock()
	defer fb.RUnlock()
	// TODO: check map key exist and fb.FieldArr boundary.
	return fb.FieldArr[fb.MapFieldToID[k]]
}

// PutFields ...
func (fb *FieldBinding) PutFields(fArr []string) {
	fCount := len(fArr)
	fb.FieldArr = make([]interface{}, fCount)
	fb.FieldPtrArr = make([]interface{}, fCount)
	fb.MapFieldToID = make(map[string]int64, fCount)

	for k, v := range fArr {
		fb.FieldPtrArr[k] = &fb.FieldArr[k]
		fb.put(v, int64(k))
	}
}

// GetFieldPtrArr ...
func (fb *FieldBinding) GetFieldPtrArr() []interface{} {
	return fb.FieldPtrArr
}

// GetFieldArr ...
func (fb *FieldBinding) GetFieldArr() map[string]interface{} {
	m := make(map[string]interface{}, fb.FieldCount)

	for k, v := range fb.MapFieldToID {
		m[k] = fb.FieldArr[v]
	}

	return m
}