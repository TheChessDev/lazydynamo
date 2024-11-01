#!/bin/bash

# Repository details
REPO="TheChessDev/lazydynamo"
INSTALL_DIR="/usr/local/bin"
EXECUTABLE_NAME="lazydynamo"

# Ensure prerequisites are installed
echo "Checking for prerequisites..."
if ! command -v git &> /dev/null; then
    echo "Error: git is not installed. Please install git and try again."
    exit 1
fi

if ! command -v go &> /dev/null; then
    echo "Error: Go is not installed. Please install Go and try again."
    exit 1
fi

# Create a temporary directory and navigate to it
TEMP_DIR=$(mktemp -d)
cd "$TEMP_DIR" || exit 1

# Clone the repository
echo "Cloning repository..."
git clone "https://github.com/$REPO.git"
cd "$EXECUTABLE_NAME" || exit 1

# Build the executable
echo "Building $EXECUTABLE_NAME..."
if ! go build -o "$EXECUTABLE_NAME"; then
    echo "Error: Build failed."
    exit 1
fi

# Move the executable to the specified installation directory
echo "Installing $EXECUTABLE_NAME to $INSTALL_DIR..."
sudo mv "$EXECUTABLE_NAME" "$INSTALL_DIR"

# Cleanup
cd ~ || exit 1
rm -rf "$TEMP_DIR"

# Confirm installation
if command -v "$EXECUTABLE_NAME" &> /dev/null; then
    echo "$EXECUTABLE_NAME successfully installed! Run it from anywhere with '$EXECUTABLE_NAME'."
else
    echo "Installation failed. Please check permissions or the installation directory."
fi
