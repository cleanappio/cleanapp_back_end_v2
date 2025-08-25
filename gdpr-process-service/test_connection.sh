#!/bin/bash

# Test database connection for GDPR Process Service

set -e

echo "Testing database connection for GDPR Process Service..."

# Load environment variables (create .env file if needed)
if [ -f .env ]; then
    source .env
fi

# Set defaults if not provided
DB_HOST=${DB_HOST:-localhost}
DB_PORT=${DB_PORT:-3306}
DB_USER=${DB_USER:-server}
DB_PASSWORD=${DB_PASSWORD:-secret}
DB_NAME=${DB_NAME:-cleanapp}

echo "Testing connection to: $DB_USER@$DB_HOST:$DB_PORT/$DB_NAME"

# Test MySQL connection
if command -v mysql &> /dev/null; then
    echo "Testing with mysql client..."
    mysql -h "$DB_HOST" -P "$DB_PORT" -u "$DB_USER" -p"$DB_PASSWORD" "$DB_NAME" -e "SELECT 1 as connection_test;" 2>/dev/null
    if [ $? -eq 0 ]; then
        echo "✅ Database connection successful!"
        
        # Check if required tables exist
        echo "Checking required tables..."
        mysql -h "$DB_HOST" -P "$DB_PORT" -u "$DB_USER" -p"$DB_PASSWORD" "$DB_NAME" -e "
        SELECT 
            TABLE_NAME,
            CASE 
                WHEN TABLE_NAME IN ('users', 'reports') THEN 'Source Table'
                WHEN TABLE_NAME IN ('users_gdpr', 'reports_gdpr') THEN 'Tracking Table'
                ELSE 'Other Table'
            END as Table_Type
        FROM information_schema.TABLES 
        WHERE TABLE_SCHEMA = '$DB_NAME' 
        AND TABLE_NAME IN ('users', 'reports', 'users_gdpr', 'reports_gdpr')
        ORDER BY TABLE_NAME;" 2>/dev/null
        
        echo "✅ Database connectivity test completed!"
    else
        echo "❌ Database connection failed!"
        exit 1
    fi
else
    echo "⚠️  mysql client not found. Skipping connection test."
    echo "Please install mysql client or test manually."
fi

echo ""
echo "To run the service:"
echo "1. Set environment variables (see README.md)"
echo "2. Run: go run main.go"
echo "3. Or build and run with Docker: ./build_image.sh && docker-compose up -d"
