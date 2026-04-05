# Webfiles Server

A simple Go HTTP server for file management and CSV-to-SQLite data storage.

## Features

- **File Management**: Upload, download, and delete files
- **CSV Data Storage**: Upload CSV files to SQLite tables, download as CSV, delete tables
- **Duplicate Protection**: Returns 409 Conflict for duplicate uploads

## Quick Start

### Build and Run

```bash
cd webfiles

# Download dependencies
go mod tidy

# Compile server
go build -o web-server main.go
./web-server --port 8080 --path .
```

The server starts on `http://localhost:8080` in the current folder.

### Directory Structure

```
webfiles/
  main.go           # Server implementation
  go.mod            # Go module definition
  files/            # Uploaded files stored here (created on startup)
  data/             # SQLite database stored here (created on startup)
  test_requests.sh  # Test script
```

## API Endpoints

### File Management

#### POST /files/upload

Upload a file. Returns 409 Conflict if file already exists.

```bash
curl -X POST -F "file=@myfile.txt" http://localhost:8080/files/upload
```

**Response**: `201 Created` on success

#### GET /files/download/{filename}

Download a file (forces download dialog).

```bash
curl -O http://localhost:8080/files/download/myfile.txt
```

**Response**: File content with `Content-Disposition: attachment` header

#### GET /files/view/{filename}

View a file in the browser (displays inline).

```bash
curl http://localhost:8080/files/view/myfile.txt
# Or open in browser: http://localhost:8080/files/view/myfile.txt
```

**Response**: File content displayed inline (no download header)

#### DELETE /files/delete/{filename}

Delete a file.

```bash
curl -X DELETE http://localhost:8080/files/delete/myfile.txt
```

**Response**: `200 OK` on success

#### GET /files/list_all

List all uploaded files.

```bash
curl http://localhost:8080/files/list_all
```

**Response**: JSON array of filenames

### CSV/SQLite Data

#### POST /data/upload

Upload a CSV file to create a SQLite table. Returns 409 Conflict if table already exists.

```bash
# Table name derived from filename (without extension)
curl -X POST -F "file=@data.csv" http://localhost:8080/data/upload

# Or specify custom table name
curl -X POST -F "file=@data.csv" -F "table_name=my_table" http://localhost:8080/data/upload
```

**Response**: `201 Created` on success

#### GET /data/download/{table_name}

Download a table as CSV.

```bash
curl -O http://localhost:8080/data/download/my_table
```

**Response**: CSV file with table data

#### DELETE /data/delete/{table_name}

Delete a SQLite table.

```bash
curl -X DELETE http://localhost:8080/data/delete/my_table
```

**Response**: `200 OK` on success

#### GET /data/list_all

List all tables in the database.

```bash
curl http://localhost:8080/data/list_all
```

**Response**: JSON array of table names

## Testing

Run the test script to verify all endpoints:

```bash
# Start the server in one terminal
go run main.go &

# Run tests in another terminal
chmod +x test_requests.sh
./test_requests.sh
```

## Notes

- All uploaded files are stored in the `files/` directory
- SQLite database is stored at `data/webfiles.db`
- Table names are sanitized to allow only alphanumeric characters and underscores
- Maximum upload size is 500MB

## Usefull commands

Kill the port on linux
```
kill -9 $(lsof -t -i :<port_number>)
```
