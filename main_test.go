package main

import (
	"bytes"
	"database/sql"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	_ "github.com/marcboeker/go-duckdb/v2"
)

var testServer *httptest.Server
var testDBPath string

func TestMain(m *testing.M) {
	// Set up test database and server
	var err error
	testDBPath, err = os.MkdirTemp("", "duckdb-test-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(testDBPath)

	dataPath = testDBPath
	filesDir = testDBPath + "/files"
	dataDir = testDBPath + "/data"
	dbFile = dataDir + "/test.db"

	if err := os.MkdirAll(filesDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create files dir: %v\n", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create data dir: %v\n", err)
		os.Exit(1)
	}

	db, err = sql.Open("duckdb", dbFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	// Create metadata table
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS "_metadata" (
		table_name VARCHAR PRIMARY KEY,
		metadata JSON,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create metadata table: %v\n", err)
		os.Exit(1)
	}

	// Set up routes
	mux := http.NewServeMux()
	mux.HandleFunc("/files/list_all", methodHandler(http.MethodGet, handleFileList))
	mux.HandleFunc("/files/upload", methodHandler(http.MethodPost, handleFileUpload))
	mux.HandleFunc("/files/download/", methodHandler(http.MethodGet, handleFileDownload))
	mux.HandleFunc("/files/view/", methodHandler(http.MethodGet, handleFileView))
	mux.HandleFunc("/files/delete/", methodHandler(http.MethodDelete, handleFileDelete))
	mux.HandleFunc("/data/list_all", methodHandler(http.MethodGet, handleDataList))
	mux.HandleFunc("/data/upload", methodHandler(http.MethodPost, handleDataUpload))
	mux.HandleFunc("/data/download/", methodHandler(http.MethodGet, handleDataDownload))
	mux.HandleFunc("/data/delete/", methodHandler(http.MethodDelete, handleDataDelete))
	mux.HandleFunc("/data/query/", methodHandler(http.MethodGet, handleDataQuery))
	mux.HandleFunc("/data/metadata/", handleDataMetadata)

	testServer = httptest.NewServer(mux)
	defer testServer.Close()

	code := m.Run()
	os.Exit(code)
}

// Helper functions

func uploadFile(t *testing.T, filename, content string) int {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("Failed to create form file: %v", err)
	}
	part.Write([]byte(content))
	writer.Close()

	req, _ := http.NewRequest(http.MethodPost, testServer.URL+"/files/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to upload file: %v", err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func uploadCSV(t *testing.T, filename, csvContent, tableName, metadata string) (int, string) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("Failed to create form file: %v", err)
	}
	part.Write([]byte(csvContent))

	if tableName != "" {
		writer.WriteField("table_name", tableName)
	}
	if metadata != "" {
		writer.WriteField("metadata", metadata)
	}
	writer.Close()

	req, _ := http.NewRequest(http.MethodPost, testServer.URL+"/data/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to upload CSV: %v", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(respBody)
}

func getJSON(t *testing.T, url string) (int, string) {
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("Failed to GET: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

func deleteResource(t *testing.T, url string) int {
	req, _ := http.NewRequest(http.MethodDelete, url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to delete: %v", err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// File Management Tests

func TestFileUpload(t *testing.T) {
	status := uploadFile(t, "test.txt", "Hello, World!")
	if status != http.StatusCreated {
		t.Errorf("File upload returned %d, expected %d", status, http.StatusCreated)
	}
}

func TestFileUploadDuplicate(t *testing.T) {
	uploadFile(t, "duplicate.txt", "First upload")
	status := uploadFile(t, "duplicate.txt", "Second upload")
	if status != http.StatusConflict {
		t.Errorf("Duplicate file upload returned %d, expected %d", status, http.StatusConflict)
	}
}

func TestFileDownload(t *testing.T) {
	content := "Test file content"
	uploadFile(t, "download_test.txt", content)

	resp, err := http.Get(testServer.URL + "/files/download/download_test.txt")
	if err != nil {
		t.Fatalf("Failed to download: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("File download returned %d, expected %d", resp.StatusCode, http.StatusOK)
	}
	if string(body) != content {
		t.Errorf("Downloaded content mismatch: got %q, want %q", string(body), content)
	}
}

func TestFileDownloadNotFound(t *testing.T) {
	status, _ := getJSON(t, testServer.URL+"/files/download/nonexistent.txt")
	if status != http.StatusNotFound {
		t.Errorf("Non-existent file download returned %d, expected %d", status, http.StatusNotFound)
	}
}

func TestFileDelete(t *testing.T) {
	uploadFile(t, "delete_test.txt", "To be deleted")
	status := deleteResource(t, testServer.URL+"/files/delete/delete_test.txt")
	if status != http.StatusOK {
		t.Errorf("File delete returned %d, expected %d", status, http.StatusOK)
	}

	// Verify file is gone
	status, _ = getJSON(t, testServer.URL+"/files/download/delete_test.txt")
	if status != http.StatusNotFound {
		t.Error("File still exists after deletion")
	}
}

func TestFileDeleteNotFound(t *testing.T) {
	status := deleteResource(t, testServer.URL+"/files/delete/nonexistent.txt")
	if status != http.StatusNotFound {
		t.Errorf("Non-existent file delete returned %d, expected %d", status, http.StatusNotFound)
	}
}

func TestFileView(t *testing.T) {
	content := "Viewable content"
	uploadFile(t, "view_test.txt", content)

	resp, err := http.Get(testServer.URL + "/files/view/view_test.txt")
	if err != nil {
		t.Fatalf("Failed to view: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("File view returned %d, expected %d", resp.StatusCode, http.StatusOK)
	}
	if string(body) != content {
		t.Errorf("Viewed content mismatch: got %q, want %q", string(body), content)
	}
}

func TestFileList(t *testing.T) {
	uploadFile(t, "list_test1.txt", "File 1")
	uploadFile(t, "list_test2.txt", "File 2")

	status, body := getJSON(t, testServer.URL+"/files/list_all")
	if status != http.StatusOK {
		t.Errorf("List files returned %d, expected %d", status, http.StatusOK)
	}
	if !strings.Contains(body, "list_test1.txt") || !strings.Contains(body, "list_test2.txt") {
		t.Errorf("List files missing expected files: %s", body)
	}
}

func TestFileListMethodNotAllowed(t *testing.T) {
	resp, err := http.Post(testServer.URL+"/files/list_all", "text/plain", nil)
	if err != nil {
		t.Fatalf("Failed to POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("List files with POST returned %d, expected %d", resp.StatusCode, http.StatusMethodNotAllowed)
	}
}

// Data Management Tests

func TestDataUpload(t *testing.T) {
	csvContent := "name,value\ntest,123"
	status, _ := uploadCSV(t, "upload_test.csv", csvContent, "", "")
	if status != http.StatusCreated {
		t.Errorf("CSV upload returned %d, expected %d", status, http.StatusCreated)
	}
}

func TestDataUploadDuplicate(t *testing.T) {
	csvContent := "name,value\ntest,123"
	uploadCSV(t, "dup_test.csv", csvContent, "", "")
	status, _ := uploadCSV(t, "dup_test.csv", csvContent, "", "")
	if status != http.StatusConflict {
		t.Errorf("Duplicate CSV upload returned %d, expected %d", status, http.StatusConflict)
	}
}

func TestDataUploadWithCustomName(t *testing.T) {
	csvContent := "name,value\ntest,123"
	status, body := uploadCSV(t, "custom.csv", csvContent, "custom_table_name", "")
	if status != http.StatusCreated {
		t.Errorf("CSV upload with custom name returned %d, expected %d: %s", status, http.StatusCreated, body)
	}
}

func TestDataUploadWithMetadata(t *testing.T) {
	csvContent := "name,value\ntest,123"
	metadata := `{"description":"test data","version":1}`
	status, body := uploadCSV(t, "metadata_test.csv", csvContent, "metadata_table", metadata)
	if status != http.StatusCreated {
		t.Errorf("CSV upload with metadata returned %d, expected %d: %s", status, http.StatusCreated, body)
	}

	// Verify metadata was stored
	resp, err := http.Get(testServer.URL + "/data/metadata/metadata_table")
	if err != nil {
		t.Fatalf("Failed to get metadata: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Metadata GET returned %d, expected %d", resp.StatusCode, http.StatusOK)
	}

	metadataBody, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(metadataBody), "test data") {
		t.Errorf("Metadata not stored correctly: %s", string(metadataBody))
	}
}

func TestDataDownload(t *testing.T) {
	csvContent := "name,value\ntest,123"
	uploadCSV(t, "download_data.csv", csvContent, "download_data", "")

	resp, err := http.Get(testServer.URL + "/data/download/download_data")
	if err != nil {
		t.Fatalf("Failed to download data: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Data download returned %d, expected %d", resp.StatusCode, http.StatusOK)
	}
}

func TestDataDownloadNotFound(t *testing.T) {
	status, _ := getJSON(t, testServer.URL+"/data/download/nonexistent")
	if status != http.StatusNotFound {
		t.Errorf("Non-existent table download returned %d, expected %d", status, http.StatusNotFound)
	}
}

func TestDataDelete(t *testing.T) {
	csvContent := "name,value\ntest,123"
	uploadCSV(t, "delete_data.csv", csvContent, "delete_data", "")

	status := deleteResource(t, testServer.URL+"/data/delete/delete_data")
	if status != http.StatusOK {
		t.Errorf("Table delete returned %d, expected %d", status, http.StatusOK)
	}

	// Verify table is gone
	status, _ = getJSON(t, testServer.URL+"/data/download/delete_data")
	if status != http.StatusNotFound {
		t.Error("Table still exists after deletion")
	}
}

func TestDataDeleteNotFound(t *testing.T) {
	status := deleteResource(t, testServer.URL+"/data/delete/nonexistent")
	if status != http.StatusNotFound {
		t.Errorf("Non-existent table delete returned %d, expected %d", status, http.StatusNotFound)
	}
}

func TestDataQuery(t *testing.T) {
	csvContent := "name,value\ntest1,100\ntest2,200"
	uploadCSV(t, "query_data.csv", csvContent, "query_data", "")

	status, body := getJSON(t, testServer.URL+"/data/query/query_data")
	if status != http.StatusOK {
		t.Errorf("Data query returned %d, expected %d", status, http.StatusOK)
	}
	if !strings.Contains(body, "test1") || !strings.Contains(body, "test2") {
		t.Errorf("Data query missing expected data: %s", body)
	}
}

func TestDataList(t *testing.T) {
	csvContent := "name,value\ntest,123"
	uploadCSV(t, "list_data.csv", csvContent, "list_data", "")

	status, body := getJSON(t, testServer.URL+"/data/list_all")
	if status != http.StatusOK {
		t.Errorf("List tables returned %d, expected %d", status, http.StatusOK)
	}
	if !strings.Contains(body, "list_data") {
		t.Errorf("List tables missing expected table: %s", body)
	}
}

func TestDataListMethodNotAllowed(t *testing.T) {
	resp, err := http.Post(testServer.URL+"/data/list_all", "text/plain", nil)
	if err != nil {
		t.Fatalf("Failed to POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("List tables with POST returned %d, expected %d", resp.StatusCode, http.StatusMethodNotAllowed)
	}
}

// Metadata Tests

func TestMetadataGet(t *testing.T) {
	csvContent := "name,value\ntest,123"
	metadata := `{"key":"value","nested":{"a":1}}`
	uploadCSV(t, "meta_get_test.csv", csvContent, "meta_get_test", metadata)

	status, body := getJSON(t, testServer.URL+"/data/metadata/meta_get_test")
	if status != http.StatusOK {
		t.Errorf("Metadata GET returned %d, expected %d", status, http.StatusOK)
	}
	if !strings.Contains(body, "key") || !strings.Contains(body, "value") {
		t.Errorf("Metadata GET returned unexpected body: %s", body)
	}
}

func TestMetadataGetNotFound(t *testing.T) {
	status, _ := getJSON(t, testServer.URL+"/data/metadata/nonexistent")
	if status != http.StatusNotFound {
		t.Errorf("Metadata GET for nonexistent table returned %d, expected %d", status, http.StatusNotFound)
	}
}

func TestMetadataPut(t *testing.T) {
	csvContent := "name,value\ntest,123"
	uploadCSV(t, "meta_put_test.csv", csvContent, "meta_put_test", "")

	metadata := `{"updated":"true"}`
	req, _ := http.NewRequest(http.MethodPut, testServer.URL+"/data/metadata/meta_put_test", strings.NewReader(metadata))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to PUT metadata: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Metadata PUT returned %d, expected %d", resp.StatusCode, http.StatusOK)
	}

	// Verify update
	status, body := getJSON(t, testServer.URL+"/data/metadata/meta_put_test")
	if status != http.StatusOK {
		t.Errorf("Metadata GET after PUT returned %d, expected %d", status, http.StatusOK)
	}
	if !strings.Contains(body, "updated") {
		t.Errorf("Metadata PUT did not update: %s", body)
	}
}

func TestMetadataDelete(t *testing.T) {
	csvContent := "name,value\ntest,123"
	metadata := `{"to":"delete"}`
	uploadCSV(t, "meta_del_test.csv", csvContent, "meta_del_test", metadata)

	status := deleteResource(t, testServer.URL+"/data/metadata/meta_del_test")
	if status != http.StatusOK {
		t.Errorf("Metadata DELETE returned %d, expected %d", status, http.StatusOK)
	}

	// Verify deletion
	status, _ = getJSON(t, testServer.URL+"/data/metadata/meta_del_test")
	if status != http.StatusNotFound {
		t.Error("Metadata still exists after deletion")
	}
}
