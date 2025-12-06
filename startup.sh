#!/bin/sh

# Path where the Kubernetes Secret is mounted
ENV_FILE="/workspace/.env"

# Check if the .env file exists and source it
if [ -f "$ENV_FILE" ]; then
    echo "Loading environment variables from $ENV_FILE"
    # Source the .env file to load variables into the shell environment
    . "$ENV_FILE"
fi

# Execute the application binary.
# The actual name of the compiled binary is provided by Paketo via the '$@' argument, 
# which represents the remaining command (the original process).
echo "Starting application with command: $@"
exec "$@"
