package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
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
		Types		 map[string]string `json:"types"`
		Results      []map[string]interface{} `json:"results"`
		ExecDuration int64  `json:"exec_duration"`
		MemUsage     int64  `json:"mem_usage"`

	}
	CustomScanner struct {
		columnType string
		valid bool
		value interface{}
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
	db, err := sql.Open("mysql", a.dbusername+":"+a.dbpassword+"@tcp(localhost:3306)/"+req.Database); if err !=nil {
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

	columnNames, err := rows.Columns(); if err != nil {
		a.badRequest(w, err)
		return nil
	}

	columnTypes, err := rows.ColumnTypes(); if err != nil {
		a.badRequest(w, err)
		return nil
	}

	columnTypeMap := make(map[string]string)
	for i, name := range columnNames {
		dataType := columnTypes[i].DatabaseTypeName()
		columnTypeMap[name] = dataType
	}


	numCols := len(columnNames)
	log.Printf("result has %d columns\n", numCols)
	var results []map[string]interface{}
	for rows.Next() {
		columns := make([]interface{}, len(columnNames))
		for idx := range columnNames {
            columns[idx] = new(CustomScanner)
			columns[idx].(*CustomScanner).columnType = columnTypes[idx].DatabaseTypeName()
        }
		
		err := rows.Scan(columns...)
        if err != nil {
            a.badRequest(w, err)
			return nil
        }

		row := make(map[string]interface{})
        for idx, column := range columnNames {
            var scanner = columns[idx].(*CustomScanner)
            // log.Println(column, ":", scanner.value)
			if !scanner.valid {
				a.badRequest(w, errors.New("Failed to scan column " + column + " of type " + scanner.columnType + " with value " + string(scanner.getBytes(scanner.value)) + " to interface{}"))
				return nil
			}
			row[column] = scanner.value
        }
		results = append(results, row)

	}

	if err := rows.Err(); err != nil {
		a.badRequest(w, err)
        return nil
    }
	
	WriteJSON(w, http.StatusOK, APIClientResponse{
			Message: "Success",
			Columns: columnNames,
			Types: columnTypeMap,
			Results: results,
			ExecDuration: elapsed.Milliseconds(),
	})
	log.Printf("done with db connection")
	
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
	compileCmd := exec.Command("mysql", req.Database, "--disable-auto-rehash",  "-e", "source ./tmp/"+req.ID)
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

// --- any custom scanner ---
func (scanner *CustomScanner) getBytes(src interface{}) []byte {
    if a, ok := src.([]uint8); ok {
        return a
    }
    return nil
}

func (scanner *CustomScanner) Scan(src interface{}) error {
	switch src.(type) {
	case []byte:
		value := scanner.getBytes(src)
		scanner.valid = true
		switch scanner.columnType {
		case "VARCHAR", "TEXT", "CHAR", "TINYTEXT", "MEDIUMTEXT", "LONGTEXT", "ENUM":
			scanner.value = string(value)
		case "UNSIGNED BIGINT", "UNSIGNED INT", "UNSIGNED TINYINT", "UNSIGNED SMALLINT", "UNSIGNED MEDIUMINT":
			if len(value) == 0 {
				scanner.value = 0
			} else {
				num, err := strconv.Atoi(string(value))
				if err != nil {
					return err
				}
				scanner.value = num
			}
		case "INT", "TINYINT", "SMALLINT", "MEDIUMINT", "BIGINT":
			if len(value) == 0 {
				scanner.value = 0
			} else {
				num, err := strconv.Atoi(string(value))
				if err != nil {
					return err
				}
				scanner.value = num
			}
		case "FLOAT", "DOUBLE", "DECIMAL":
			if len(value) == 0 {
				scanner.value = 0.0
			} else {
				num, err := strconv.ParseFloat(string(value), 64)
				if err != nil {
					return err
				}
				scanner.value = num
			}
		case "DATE", "TIME", "YEAR", "DATETIME", "TIMESTAMP":
			if len(value) == 0 {
				scanner.value = time.Time{}
			} else {
				t, err := time.Parse("2006-01-02 15:04:05", string(value))
				if err != nil {
					return err
				}
				scanner.value = t
			}
		case "JSON":
			scanner.value = string(value)
		case "BIT" :
			if len(value) == 0 {
				scanner.value = false
			}
			if value[0] == 1 {
				scanner.value = true
			} else {
				scanner.value = false
			}
		case "BOOL", "BOOLEAN":
			if len(value) == 0 {
				scanner.value = false
			} else {
				b, err := strconv.ParseBool(string(value))
				if err != nil {
					return err
				}
				scanner.value = b
			}
		case "BLOB", "TINYBLOB", "MEDIUMBLOB", "LONGBLOB", "BINARY", "VARBINARY":
			scanner.value = value
			// "GEOMETRY", "POINT", "LINESTRING", "POLYGON", "MULTIPOINT", "MULTILINESTRING", "MULTIPOLYGON", "GEOMETRYCOLLECTION":
		default:
			//FIXME TEMPORARY mark unknown as invalid
			scanner.value = value
			scanner.valid = false
		}
	case nil:
		scanner.value, scanner.valid = nil, true
	}

	return nil
}