#!/bin/bash

# Repository details
REPO="TheChessDev/lazydynamo"
EXECUTABLE_NAME="lazydynamo"
BUILD_PATH="cmd/main/main.go"

# Define the installation directory
INSTALL_DIR="$HOME/.lazydynamo/bin"
mkdir -p "$INSTALL_DIR"

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
if ! go build -o "$EXECUTABLE_NAME" "$BUILD_PATH"; then
    echo "Error: Build failed."
    exit 1
fi

# Move the executable to the specified installation directory
echo "Installing $EXECUTABLE_NAME to $INSTALL_DIR..."
sudo mv "$EXECUTABLE_NAME" "$INSTALL_DIR"

# Cleanup
cd ~ || exit 1
rm -rf "$TEMP_DIR"

# Print success message
echo "lazydynamo has been installed to $INSTALL_DIR."

# Check if the installation directory is already in the PATH
if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
    SHELL_CONFIG=""
    if [[ -n "$ZSH_VERSION" ]]; then
        SHELL_CONFIG="$HOME/.zshrc"
    elif [[ -n "$BASH_VERSION" ]]; then
        SHELL_CONFIG="$HOME/.bashrc"
    else
        SHELL_CONFIG="$HOME/.profile"
    fi

    echo "To make lazydynamo globally accessible, add the following line to your $SHELL_CONFIG:"
    echo "export PATH=\"\$PATH:$INSTALL_DIR\""

    # Optionally, add to the shell config file automatically
    read -p "Would you like to add this to your $SHELL_CONFIG now? (y/n) " choice
    if [[ "$choice" =~ ^[Yy]$ ]]; then
        echo "export PATH=\"\$PATH:$INSTALL_DIR\"" >> "$SHELL_CONFIG"
        echo "Path updated. Please restart your terminal or run 'source $SHELL_CONFIG' to apply changes."
    else
        echo "You chose not to modify $SHELL_CONFIG. Please remember to add $INSTALL_DIR to your PATH manually."
    fi
else
    echo "$INSTALL_DIR is already in your PATH."
fi
