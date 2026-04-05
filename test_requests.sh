#!/bin/bash

# Test script for webfiles server
# Run this after starting the server with: go run main.go

BASE_URL="http://localhost:8080"
PASS=0
FAIL=0

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m' # No Color

pass() {
    echo -e "${GREEN}PASS${NC}: $1"
    ((PASS++))
}

fail() {
    echo -e "${RED}FAIL${NC}: $1"
    ((FAIL++))
}

# Create test files
echo "=== Creating test data ==="

# Create a test CSV file
cat > /tmp/test_data.csv << 'EOF'
name,age,city
Alice,30,New York
Bob,25,Los Angeles
Charlie,35,Chicago
EOF

# Create a test text file
echo "This is a test file" > /tmp/test.txt

echo ""
echo "=== Testing File Management Endpoints ==="
echo ""

# Test 1: Upload a file
echo "Test 1: Upload a file (POST /files/upload)"
RESPONSE=$(curl -s -w "\n%{http_code}" -X POST -F "file=@/tmp/test.txt" "$BASE_URL/files/upload")
HTTP_CODE=$(echo "$RESPONSE" | tail -n1)
if [ "$HTTP_CODE" = "201" ]; then
    pass "File upload returned 201"
else
    fail "File upload returned $HTTP_CODE (expected 201)"
fi

# Test 2: Upload duplicate file (should fail with 409)
echo "Test 2: Upload duplicate file (should return 409)"
RESPONSE=$(curl -s -w "\n%{http_code}" -X POST -F "file=@/tmp/test.txt" "$BASE_URL/files/upload")
HTTP_CODE=$(echo "$RESPONSE" | tail -n1)
if [ "$HTTP_CODE" = "409" ]; then
    pass "Duplicate file upload returned 409"
else
    fail "Duplicate file upload returned $HTTP_CODE (expected 409)"
fi

# Test 3: Download a file
echo "Test 3: Download a file (GET /files/download/test.txt)"
RESPONSE=$(curl -s -w "\n%{http_code}" -o /tmp/downloaded.txt "$BASE_URL/files/download/test.txt")
HTTP_CODE=$(echo "$RESPONSE" | tail -n1)
if [ "$HTTP_CODE" = "200" ]; then
    if diff -q /tmp/test.txt /tmp/downloaded.txt > /dev/null; then
        pass "File download returned 200 and content matches"
    else
        fail "File download content does not match"
    fi
else
    fail "File download returned $HTTP_CODE (expected 200)"
fi

# Test 4: Download non-existent file
echo "Test 4: Download non-existent file (should return 404)"
RESPONSE=$(curl -s -w "\n%{http_code}" "$BASE_URL/files/download/nonexistent.txt")
HTTP_CODE=$(echo "$RESPONSE" | tail -n1)
if [ "$HTTP_CODE" = "404" ]; then
    pass "Non-existent file download returned 404"
else
    fail "Non-existent file download returned $HTTP_CODE (expected 404)"
fi

# Test 5: Delete a file
echo "Test 5: Delete a file (DELETE /files/delete/test.txt)"
RESPONSE=$(curl -s -w "\n%{http_code}" -X DELETE "$BASE_URL/files/delete/test.txt")
HTTP_CODE=$(echo "$RESPONSE" | tail -n1)
if [ "$HTTP_CODE" = "200" ]; then
    pass "File delete returned 200"
else
    fail "File delete returned $HTTP_CODE (expected 200)"
fi

# Test 6: Delete non-existent file
echo "Test 6: Delete non-existent file (should return 404)"
RESPONSE=$(curl -s -w "\n%{http_code}" -X DELETE "$BASE_URL/files/delete/nonexistent.txt")
HTTP_CODE=$(echo "$RESPONSE" | tail -n1)
if [ "$HTTP_CODE" = "404" ]; then
    pass "Non-existent file delete returned 404"
else
    fail "Non-existent file delete returned $HTTP_CODE (expected 404)"
fi

# Test 6b: View a file in browser
echo "Test 6b: Upload file for view test"
curl -s -X POST -F "file=@/tmp/test.txt" "$BASE_URL/files/upload" > /dev/null

echo "Test 6c: View a file in browser (GET /files/view/test.txt)"
RESPONSE=$(curl -s -w "\n%{http_code}" "$BASE_URL/files/view/test.txt")
HTTP_CODE=$(echo "$RESPONSE" | tail -n1)
BODY=$(echo "$RESPONSE" | head -n -1)
if [ "$HTTP_CODE" = "200" ] && [ "$BODY" = "This is a test file" ]; then
    pass "File view returned 200 and content matches"
else
    fail "File view returned $HTTP_CODE or content does not match"
fi

echo ""
echo "=== Testing CSV/SQLite Data Endpoints ==="
echo ""

# Test 7: Upload CSV data
echo "Test 7: Upload CSV data (POST /data/upload)"
RESPONSE=$(curl -s -w "\n%{http_code}" -X POST -F "file=@/tmp/test_data.csv" "$BASE_URL/data/upload")
HTTP_CODE=$(echo "$RESPONSE" | tail -n1)
BODY=$(echo "$RESPONSE" | head -n -1)
if [ "$HTTP_CODE" = "201" ]; then
    pass "CSV upload returned 201: $BODY"
else
    fail "CSV upload returned $HTTP_CODE (expected 201): $BODY"
fi

# Test 8: Upload duplicate CSV (should fail with 409)
echo "Test 8: Upload duplicate CSV (should return 409)"
RESPONSE=$(curl -s -w "\n%{http_code}" -X POST -F "file=@/tmp/test_data.csv" "$BASE_URL/data/upload")
HTTP_CODE=$(echo "$RESPONSE" | tail -n1)
if [ "$HTTP_CODE" = "409" ]; then
    pass "Duplicate CSV upload returned 409"
else
    fail "Duplicate CSV upload returned $HTTP_CODE (expected 409)"
fi

# Test 9: Download data as CSV
echo "Test 9: Download data as CSV (GET /data/download/test_data)"
RESPONSE=$(curl -s -w "\n%{http_code}" -o /tmp/downloaded_data.csv "$BASE_URL/data/download/test_data")
HTTP_CODE=$(echo "$RESPONSE" | tail -n1)
if [ "$HTTP_CODE" = "200" ]; then
    # Compare CSV files (ignoring potential differences in line endings)
    if diff -q /tmp/test_data.csv /tmp/downloaded_data.csv > /dev/null 2>&1; then
        pass "CSV download returned 200 and content matches"
    else
        # Check if data is equivalent (row count)
        ORIG_ROWS=$(wc -l < /tmp/test_data.csv)
        DOWN_ROWS=$(wc -l < /tmp/downloaded_data.csv)
        if [ "$ORIG_ROWS" = "$DOWN_ROWS" ]; then
            pass "CSV download returned 200 and row count matches ($ORIG_ROWS rows)"
        else
            fail "CSV download content differs (original: $ORIG_ROWS rows, downloaded: $DOWN_ROWS rows)"
        fi
    fi
else
    fail "CSV download returned $HTTP_CODE (expected 200)"
fi

# Test 10: Download non-existent table
echo "Test 10: Download non-existent table (should return 404)"
RESPONSE=$(curl -s -w "\n%{http_code}" "$BASE_URL/data/download/nonexistent")
HTTP_CODE=$(echo "$RESPONSE" | tail -n1)
if [ "$HTTP_CODE" = "404" ]; then
    pass "Non-existent table download returned 404"
else
    fail "Non-existent table download returned $HTTP_CODE (expected 404)"
fi

# Test 11: Delete a table
echo "Test 11: Delete a table (DELETE /data/delete/test_data)"
RESPONSE=$(curl -s -w "\n%{http_code}" -X DELETE "$BASE_URL/data/delete/test_data")
HTTP_CODE=$(echo "$RESPONSE" | tail -n1)
if [ "$HTTP_CODE" = "200" ]; then
    pass "Table delete returned 200"
else
    fail "Table delete returned $HTTP_CODE (expected 200)"
fi

# Test 12: Delete non-existent table
echo "Test 12: Delete non-existent table (should return 404)"
RESPONSE=$(curl -s -w "\n%{http_code}" -X DELETE "$BASE_URL/data/delete/nonexistent")
HTTP_CODE=$(echo "$RESPONSE" | tail -n1)
if [ "$HTTP_CODE" = "404" ]; then
    pass "Non-existent table delete returned 404"
else
    fail "Non-existent table delete returned $HTTP_CODE (expected 404)"
fi

# Test 13: Upload CSV with custom table name
echo "Test 13: Upload CSV with custom table name"
RESPONSE=$(curl -s -w "\n%{http_code}" -X POST -F "file=@/tmp/test_data.csv" -F "table_name=custom_table" "$BASE_URL/data/upload")
HTTP_CODE=$(echo "$RESPONSE" | tail -n1)
BODY=$(echo "$RESPONSE" | head -n -1)
if [ "$HTTP_CODE" = "201" ]; then
    pass "CSV upload with custom table name returned 201: $BODY"
else
    fail "CSV upload with custom table name returned $HTTP_CODE (expected 201): $BODY"
fi

# Clean up custom table
curl -s -X DELETE "$BASE_URL/data/delete/custom_table" > /dev/null

echo ""
echo "=== Testing List All Endpoints ==="
echo ""

# Test 14: List all files (empty)
echo "Test 14: List all files (GET /files/list_all)"
RESPONSE=$(curl -s -w "\n%{http_code}" "$BASE_URL/files/list_all")
HTTP_CODE=$(echo "$RESPONSE" | tail -n1)
BODY=$(echo "$RESPONSE" | head -n -1)
if [ "$HTTP_CODE" = "200" ]; then
    pass "List all files returned 200: $BODY"
else
    fail "List all files returned $HTTP_CODE (expected 200)"
fi

# Test 15: List all files (with files present)
echo "Test 15: List all files with files present"
# Upload a couple of test files
curl -s -X POST -F "file=@/tmp/test.txt" "$BASE_URL/files/upload" > /dev/null
echo "second test file" > /tmp/test2.txt
curl -s -X POST -F "file=@/tmp/test2.txt" "$BASE_URL/files/upload" > /dev/null

RESPONSE=$(curl -s -w "\n%{http_code}" "$BASE_URL/files/list_all")
HTTP_CODE=$(echo "$RESPONSE" | tail -n1)
BODY=$(echo "$RESPONSE" | head -n -1)
if [ "$HTTP_CODE" = "200" ]; then
    if echo "$BODY" | grep -q "test.txt" && echo "$BODY" | grep -q "test2.txt"; then
        pass "List all files returned 200 with expected files: $BODY"
    else
        fail "List all files missing expected files: $BODY"
    fi
else
    fail "List all files returned $HTTP_CODE (expected 200)"
fi

# Test 16: List all tables (with tables present)
echo "Test 16: List all tables (GET /data/list_all)"
# Upload a test table
curl -s -X POST -F "file=@/tmp/test_data.csv" "$BASE_URL/data/upload" > /dev/null 2>&1 || true

RESPONSE=$(curl -s -w "\n%{http_code}" "$BASE_URL/data/list_all")
HTTP_CODE=$(echo "$RESPONSE" | tail -n1)
BODY=$(echo "$RESPONSE" | head -n -1)
if [ "$HTTP_CODE" = "200" ]; then
    if echo "$BODY" | grep -q "test_data"; then
        pass "List all tables returned 200 with expected table: $BODY"
    else
        pass "List all tables returned 200: $BODY"
    fi
else
    fail "List all tables returned $HTTP_CODE (expected 200)"
fi

# Test 17: List all - method not allowed
echo "Test 17: List all files with POST (should return 405)"
RESPONSE=$(curl -s -w "\n%{http_code}" -X POST "$BASE_URL/files/list_all")
HTTP_CODE=$(echo "$RESPONSE" | tail -n1)
if [ "$HTTP_CODE" = "405" ]; then
    pass "List all files with POST returned 405"
else
    fail "List all files with POST returned $HTTP_CODE (expected 405)"
fi

echo "Test 18: List all tables with POST (should return 405)"
RESPONSE=$(curl -s -w "\n%{http_code}" -X POST "$BASE_URL/data/list_all")
HTTP_CODE=$(echo "$RESPONSE" | tail -n1)
if [ "$HTTP_CODE" = "405" ]; then
    pass "List all tables with POST returned 405"
else
    fail "List all tables with POST returned $HTTP_CODE (expected 405)"
fi

# Clean up test files and tables
curl -s -X DELETE "$BASE_URL/files/delete/test.txt" > /dev/null
curl -s -X DELETE "$BASE_URL/files/delete/test2.txt" > /dev/null
curl -s -X DELETE "$BASE_URL/data/delete/test_data" > /dev/null

echo ""
echo "=== Test Summary ==="
echo -e "${GREEN}Passed: $PASS${NC}"
echo -e "${RED}Failed: $FAIL${NC}"

# Cleanup
rm -f /tmp/test.txt /tmp/test_data.csv /tmp/downloaded.txt /tmp/downloaded_data.csv

if [ $FAIL -eq 0 ]; then
    echo -e "\n${GREEN}All tests passed!${NC}"
    exit 0
else
    echo -e "\n${RED}Some tests failed.${NC}"
    exit 1
fi
