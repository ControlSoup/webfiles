package main

import (
	"database/sql"
	"embed"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

//go:embed index.html plotting.html
var staticFiles embed.FS

const (
	maxFileSize = 10 << 20 // 10MB
)

var (
	dataPath string
	port     string
	filesDir string
	dataDir  string
	dbFile   string
	db       *sql.DB
)

// methodHandler wraps a handler to enforce a specific HTTP method
func methodHandler(method string, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		handler(w, r)
	}
}

// extractPathSegment extracts and sanitizes a filename/path segment from URL
func extractPathSegment(r *http.Request, prefix string) (string, error) {
	segment := strings.TrimPrefix(r.URL.Path, prefix)
	if segment == "" {
		return "", fmt.Errorf("no segment provided")
	}
	return filepath.Base(segment), nil
}

// sanitizeString removes disallowed characters from a string
// For table names: allowSpaceDash=false (alphanumeric + underscore only)
// For column names: allowSpaceDash=true (also allows space and dash)
func sanitizeString(s string, allowSpaceDash bool) string {
	var result strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			result.WriteRune(r)
		} else if allowSpaceDash && (r == ' ' || r == '-') {
			result.WriteRune(r)
		}
	}
	return result.String()
}

func main() {
	// Parse command line flags
	flag.StringVar(&dataPath, "path", ".", "Path to store files and data (defaults to current directory)")
	flag.StringVar(&port, "port", "8080", "Port to host this sever one")
	flag.Parse()
	port = ":" + port

	// Resolve to absolute path
	var err error
	dataPath, err = filepath.Abs(dataPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to resolve path: %v\n", err)
		os.Exit(1)
	}

	// Set up directories relative to data path
	filesDir = filepath.Join(dataPath, "files")
	dataDir = filepath.Join(dataPath, "data")
	dbFile = filepath.Join(dataDir, "webfiles.db")

	// Create directories if they don't exist
	if err := os.MkdirAll(filesDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create files directory: %v\n", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create data directory: %v\n", err)
		os.Exit(1)
	}

	// Initialize SQLite database
	db, err = sql.Open("sqlite3", dbFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	// Set up routes with method enforcement
	http.HandleFunc("/files/list_all", methodHandler(http.MethodGet, handleFileList))
	http.HandleFunc("/files/upload", methodHandler(http.MethodPost, handleFileUpload))
	http.HandleFunc("/files/download/", methodHandler(http.MethodGet, handleFileDownload))
	http.HandleFunc("/files/view/", methodHandler(http.MethodGet, handleFileView))
	http.HandleFunc("/files/delete/", methodHandler(http.MethodDelete, handleFileDelete))
	http.HandleFunc("/data/list_all", methodHandler(http.MethodGet, handleDataList))
	http.HandleFunc("/data/upload", methodHandler(http.MethodPost, handleDataUpload))
	http.HandleFunc("/data/download/", methodHandler(http.MethodGet, handleDataDownload))
	http.HandleFunc("/data/delete/", methodHandler(http.MethodDelete, handleDataDelete))
	http.HandleFunc("/data/query/", methodHandler(http.MethodGet, handleDataQuery))

	// Serve embedded index.html at root, static files from disk for everything else
	http.HandleFunc("/", handleStatic)
	http.HandleFunc("/data/plotting", handlePlottingPage)

	fmt.Printf("Starting server on %s\n", port)
	fmt.Printf("Data path: %s\n", dataPath)
	fmt.Printf("Files directory: %s\n", filesDir)
	fmt.Printf("Data directory: %s\n", dataDir)
	if err := http.ListenAndServe(port, nil); err != nil {
		fmt.Fprintf(os.Stderr, "Server failed: %v\n", err)
		os.Exit(1)
	}
}

// File Management Handlers

func handleFileList(w http.ResponseWriter, r *http.Request) {
	// Read files directory
	entries, err := os.ReadDir(filesDir)
	if err != nil {
		http.Error(w, "Failed to read files directory: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Filter to only files (not directories) and collect names
	var files []string
	for _, entry := range entries {
		if !entry.IsDir() {
			files = append(files, entry.Name())
		}
	}

	// Return as JSON array
	w.Header().Set("Content-Type", "application/json")
	if files == nil {
		files = []string{}
	}
	fmt.Fprintf(w, "[")
	for i, f := range files {
		if i > 0 {
			fmt.Fprintf(w, ",")
		}
		fmt.Fprintf(w, "\"%s\"", f)
	}
	fmt.Fprintf(w, "]")
}

func handleFileUpload(w http.ResponseWriter, r *http.Request) {
	// Parse multipart form (max 10MB)
	if err := r.ParseMultipartForm(maxFileSize); err != nil {
		http.Error(w, "Failed to parse form: "+err.Error(), http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Failed to get file: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Validate file size
	if header.Size > maxFileSize {
		http.Error(w, "File too large", http.StatusRequestEntityTooLarge)
		return
	}

	filename := header.Filename
	if filename == "" {
		http.Error(w, "No filename provided", http.StatusBadRequest)
		return
	}

	// Sanitize filename
	filename = filepath.Base(filename)

	// Check if file already exists
	destPath := filepath.Join(filesDir, filename)
	if _, err := os.Stat(destPath); err == nil {
		http.Error(w, "File already exists", http.StatusConflict)
		return
	}

	// Create destination file
	dst, err := os.Create(destPath)
	if err != nil {
		http.Error(w, "Failed to create file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	// Copy file contents
	if _, err := io.Copy(dst, file); err != nil {
		http.Error(w, "Failed to save file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	fmt.Fprintf(w, "File uploaded successfully: %s\n", filename)
}

func handleFileDownload(w http.ResponseWriter, r *http.Request) {
	// Extract filename from URL path
	filename, err := extractPathSegment(r, "/files/download/")
	if err != nil {
		http.Error(w, "No filename provided", http.StatusBadRequest)
		return
	}

	filePath := filepath.Join(filesDir, filename)

	// Check if file exists
	info, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "Error accessing file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if info.IsDir() {
		http.Error(w, "Cannot download directory", http.StatusBadRequest)
		return
	}

	// Serve file
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	http.ServeFile(w, r, filePath)
}

func handleFileView(w http.ResponseWriter, r *http.Request) {
	// Extract filename from URL path
	filename, err := extractPathSegment(r, "/files/view/")
	if err != nil {
		http.Error(w, "No filename provided", http.StatusBadRequest)
		return
	}

	filePath := filepath.Join(filesDir, filename)

	// Check if file exists
	info, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "Error accessing file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if info.IsDir() {
		http.Error(w, "Cannot view directory", http.StatusBadRequest)
		return
	}

	// Serve file inline (no Content-Disposition header - displays in browser)
	http.ServeFile(w, r, filePath)
}

func handleFileDelete(w http.ResponseWriter, r *http.Request) {
	// Extract filename from URL path
	filename, err := extractPathSegment(r, "/files/delete/")
	if err != nil {
		http.Error(w, "No filename provided", http.StatusBadRequest)
		return
	}

	filePath := filepath.Join(filesDir, filename)

	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	// Delete file
	if err := os.Remove(filePath); err != nil {
		http.Error(w, "Failed to delete file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "File deleted successfully: %s\n", filename)
}

// CSV/SQLite Data Handlers

func handleDataList(w http.ResponseWriter, r *http.Request) {
	// Query all user tables from sqlite_master
	rows, err := db.Query("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
	if err != nil {
		http.Error(w, "Database error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	// Collect table names
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			http.Error(w, "Failed to scan table name: "+err.Error(), http.StatusInternalServerError)
			return
		}
		tables = append(tables, name)
	}

	// Return as JSON array
	w.Header().Set("Content-Type", "application/json")
	if tables == nil {
		tables = []string{}
	}
	fmt.Fprintf(w, "[")
	for i, t := range tables {
		if i > 0 {
			fmt.Fprintf(w, ",")
		}
		fmt.Fprintf(w, "\"%s\"", t)
	}
	fmt.Fprintf(w, "]")
}

func handleDataUpload(w http.ResponseWriter, r *http.Request) {
	// Parse multipart form (max 10MB)
	if err := r.ParseMultipartForm(maxFileSize); err != nil {
		http.Error(w, "Failed to parse form: "+err.Error(), http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Failed to get file: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Validate file size
	if header.Size > maxFileSize {
		http.Error(w, "File too large", http.StatusRequestEntityTooLarge)
		return
	}

	// Get table name from form or use filename
	tableName := r.FormValue("table_name")
	if tableName == "" {
		// Use filename without extension as table name
		tableName = strings.TrimSuffix(header.Filename, filepath.Ext(header.Filename))
	}

	// Sanitize table name (only allow alphanumeric and underscore)
	tableName = sanitizeString(tableName, false)
	if tableName == "" {
		http.Error(w, "Invalid table name", http.StatusBadRequest)
		return
	}

	// Check if table already exists
	exists, err := tableExists(tableName)
	if err != nil {
		http.Error(w, "Database error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if exists {
		http.Error(w, "Table already exists", http.StatusConflict)
		return
	}

	// Read CSV
	reader := csv.NewReader(file)
	headers, err := reader.Read()
	if err != nil {
		http.Error(w, "Failed to read CSV headers: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Create table
	columns := make([]string, len(headers))
	for i, h := range headers {
		columns[i] = fmt.Sprintf("\"%s\" TEXT", sanitizeString(h, true))
	}
	createSQL := fmt.Sprintf("CREATE TABLE \"%s\" (%s)", tableName, strings.Join(columns, ", "))

	_, err = db.Exec(createSQL)
	if err != nil {
		http.Error(w, "Failed to create table: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Begin transaction for bulk insert
	tx, err := db.Begin()
	if err != nil {
		http.Error(w, "Failed to begin transaction: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	// Prepare insert statement
	placeholders := strings.Repeat("?,", len(headers)-1) + "?"
	insertSQL := fmt.Sprintf("INSERT INTO \"%s\" VALUES (%s)", tableName, placeholders)
	stmt, err := tx.Prepare(insertSQL)
	if err != nil {
		http.Error(w, "Failed to prepare insert statement: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer stmt.Close()

	// Insert all rows
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			http.Error(w, "Failed to read CSV record: "+err.Error(), http.StatusBadRequest)
			return
		}

		if len(record) != len(headers) {
			http.Error(w, fmt.Sprintf("Record has %d fields, expected %d", len(record), len(headers)), http.StatusBadRequest)
			return
		}

		values := make([]interface{}, len(record))
		for i, v := range record {
			values[i] = v
		}

		_, err = stmt.Exec(values...)
		if err != nil {
			http.Error(w, "Failed to insert record: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		http.Error(w, "Failed to commit transaction: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	fmt.Fprintf(w, "Table created successfully: %s (%d columns, headers: %v)\n", tableName, len(headers), headers)
}

func handleDataDownload(w http.ResponseWriter, r *http.Request) {
	// Extract table name from URL path
	tableName, err := extractPathSegment(r, "/data/download/")
	if err != nil {
		http.Error(w, "No table name provided", http.StatusBadRequest)
		return
	}

	// Sanitize table name (no spaces/dashes allowed)
	tableName = sanitizeString(tableName, false)
	if tableName == "" {
		http.Error(w, "Invalid table name", http.StatusBadRequest)
		return
	}

	// Check if table exists
	exists, err := tableExists(tableName)
	if err != nil {
		http.Error(w, "Database error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if !exists {
		http.Error(w, "Table not found", http.StatusNotFound)
		return
	}

	// Query all data
	rows, err := db.Query(fmt.Sprintf("SELECT * FROM \"%s\"", tableName))
	if err != nil {
		http.Error(w, "Failed to query table: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	// Get column names
	columns, err := rows.Columns()
	if err != nil {
		http.Error(w, "Failed to get columns: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Write CSV response
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s.csv\"", tableName))
	writer := csv.NewWriter(w)

	// Write headers
	if err := writer.Write(columns); err != nil {
		return
	}

	// Write rows
	values := make([]sql.NullString, len(columns))
	scanArgs := make([]interface{}, len(columns))
	for i := range values {
		scanArgs[i] = &values[i]
	}

	for rows.Next() {
		if err := rows.Scan(scanArgs...); err != nil {
			http.Error(w, "Failed to scan row: "+err.Error(), http.StatusInternalServerError)
			return
		}

		record := make([]string, len(columns))
		for i, v := range values {
			if v.Valid {
				record[i] = v.String
			}
		}
		if err := writer.Write(record); err != nil {
			return
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		http.Error(w, "Failed to write CSV: "+err.Error(), http.StatusInternalServerError)
		return
	}
}

func handleDataDelete(w http.ResponseWriter, r *http.Request) {
	// Extract table name from URL path
	tableName, err := extractPathSegment(r, "/data/delete/")
	if err != nil {
		http.Error(w, "No table name provided", http.StatusBadRequest)
		return
	}

	// Sanitize table name (no spaces/dashes allowed)
	tableName = sanitizeString(tableName, false)
	if tableName == "" {
		http.Error(w, "Invalid table name", http.StatusBadRequest)
		return
	}

	// Check if table exists
	exists, err := tableExists(tableName)
	if err != nil {
		http.Error(w, "Database error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if !exists {
		http.Error(w, "Table not found", http.StatusNotFound)
		return
	}

	// Drop table
	_, err = db.Exec(fmt.Sprintf("DROP TABLE \"%s\"", tableName))
	if err != nil {
		http.Error(w, "Failed to delete table: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Table deleted successfully: %s\n", tableName)
}

func handleDataQuery(w http.ResponseWriter, r *http.Request) {
	// Extract table name from URL path
	tableName, err := extractPathSegment(r, "/data/query/")
	if err != nil {
		http.Error(w, "No table name provided", http.StatusBadRequest)
		return
	}

	// Sanitize table name (no spaces/dashes allowed)
	tableName = sanitizeString(tableName, false)
	if tableName == "" {
		http.Error(w, "Invalid table name", http.StatusBadRequest)
		return
	}

	// Check if table exists
	exists, err := tableExists(tableName)
	if err != nil {
		http.Error(w, "Database error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if !exists {
		http.Error(w, "Table not found", http.StatusNotFound)
		return
	}

	// Query all data
	rows, err := db.Query(fmt.Sprintf("SELECT * FROM \"%s\"", tableName))
	if err != nil {
		http.Error(w, "Failed to query table: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	// Get column names
	columns, err := rows.Columns()
	if err != nil {
		http.Error(w, "Failed to get columns: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Build JSON response
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, "{\"columns\":[")
	for i, col := range columns {
		if i > 0 {
			fmt.Fprintf(w, ",")
		}
		fmt.Fprintf(w, "\"%s\"", col)
	}
	fmt.Fprintf(w, "],\"data\":[")

	// Scan rows
	values := make([]sql.NullString, len(columns))
	scanArgs := make([]interface{}, len(columns))
	for i := range values {
		scanArgs[i] = &values[i]
	}

	firstRow := true
	for rows.Next() {
		if err := rows.Scan(scanArgs...); err != nil {
			http.Error(w, "Failed to scan row: "+err.Error(), http.StatusInternalServerError)
			return
		}

		if !firstRow {
			fmt.Fprintf(w, ",")
		}
		firstRow = false

		fmt.Fprintf(w, "{")
		for i, col := range columns {
			if i > 0 {
				fmt.Fprintf(w, ",")
			}
			val := ""
			if values[i].Valid {
				val = values[i].String
			}
			fmt.Fprintf(w, "\"%s\":\"%s\"", col, val)
		}
		fmt.Fprintf(w, "}")
	}
	fmt.Fprintf(w, "]}")
}

// Static file handler - serves embedded index.html at root
func handleStatic(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" || r.URL.Path == "/index.html" {
		// Serve embedded index.html
		content, err := staticFiles.ReadFile("index.html")
		if err != nil {
			http.Error(w, "Failed to load index.html", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write(content)
		return
	}

	// Serve other static files from data path (if needed)
	http.ServeFile(w, r, filepath.Join(dataPath, r.URL.Path))
}

// Plotting page handler - serves the standalone plotting UI
func handlePlottingPage(w http.ResponseWriter, r *http.Request) {
	// Serve embedded plotting.html
	content, err := staticFiles.ReadFile("plotting.html")
	if err != nil {
		http.Error(w, "Failed to load plotting.html", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.Write(content)
}

// Helper functions

func tableExists(tableName string) (bool, error) {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", tableName).Scan(&count)
	return count > 0, err
}
