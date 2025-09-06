#!/bin/bash

# Terraform Registry Mirror Deployment Script
# This script sets up the terraform mirror on a Linux system

set -euo pipefail

# Configuration
INSTALL_DIR="/opt/tf-mirror"
DATA_DIR="/var/lib/tf-mirror"
USER="terraform"
GROUP="terraform"
SERVICE_FILES_DIR="/etc/systemd/system"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Logging functions
log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

log_step() {
    echo -e "${BLUE}[STEP]${NC} $1"
}

# Check if running as root
check_root() {
    if [[ $EUID -ne 0 ]]; then
        log_error "This script must be run as root"
        exit 1
    fi
}

# Check system requirements
check_requirements() {
    log_step "Checking system requirements..."

    # Check if systemd is available
    if ! command -v systemctl &> /dev/null; then
        log_error "systemctl not found. This script requires systemd."
        exit 1
    fi

    # Check available disk space (require at least 10GB)
    available_space=$(df / | awk 'NR==2 {print $4}')
    required_space=$((10 * 1024 * 1024)) # 10GB in KB

    if [[ $available_space -lt $required_space ]]; then
        log_warn "Available disk space is less than 10GB. Terraform providers can take significant space."
        read -p "Do you want to continue? (y/N): " -n 1 -r
        echo
        if [[ ! $REPLY =~ ^[Yy]$ ]]; then
            exit 1
        fi
    fi

    log_info "System requirements check passed"
}

# Create user and group
create_user() {
    log_step "Creating user and group..."

    if ! getent group "$GROUP" > /dev/null 2>&1; then
        groupadd --system "$GROUP"
        log_info "Created group: $GROUP"
    else
        log_info "Group $GROUP already exists"
    fi

    if ! getent passwd "$USER" > /dev/null 2>&1; then
        useradd --system --gid "$GROUP" --home-dir "$INSTALL_DIR" \
                --no-create-home --shell /bin/false "$USER"
        log_info "Created user: $USER"
    else
        log_info "User $USER already exists"
    fi
}

# Create directories
create_directories() {
    log_step "Creating directories..."

    mkdir -p "$INSTALL_DIR"/{bin,config,logs}
    mkdir -p "$DATA_DIR"

    # Set ownership and permissions
    chown -R "$USER:$GROUP" "$INSTALL_DIR"
    chown -R "$USER:$GROUP" "$DATA_DIR"
    chmod 755 "$INSTALL_DIR"
    chmod 755 "$DATA_DIR"

    log_info "Created directories:"
    log_info "  Install: $INSTALL_DIR"
    log_info "  Data: $DATA_DIR"
}

# Install binaries
install_binaries() {
    log_step "Installing binaries..."

    # Check if binary exists in current directory
    if [[ ! -f "./bin/tf-mirror" ]]; then
        log_error "Binary not found in ./bin/"
        log_error "Please run 'make build' first"
        exit 1
    fi

    cp ./bin/tf-mirror "$INSTALL_DIR/bin/"

    # Set permissions
    chown "$USER:$GROUP" "$INSTALL_DIR/bin"/*
    chmod 755 "$INSTALL_DIR/bin"/*

    log_info "Installed binaries to $INSTALL_DIR/bin/"
}

# Install systemd services
install_services() {
    log_step "Installing systemd services..."

    # Check if service files exist
    if [[ ! -f "./examples/systemd/tf-mirror-downloader.service" ]] || \
       [[ ! -f "./examples/systemd/tf-mirror-server.service" ]]; then
        log_error "Systemd service files not found in ./examples/systemd/"
        exit 1
    fi

    # Copy service files
    cp ./examples/systemd/tf-mirror-downloader.service "$SERVICE_FILES_DIR/"
    cp ./examples/systemd/tf-mirror-server.service "$SERVICE_FILES_DIR/"

    # Update paths in service files
    sed -i "s|/opt/tf-mirror|$INSTALL_DIR|g" "$SERVICE_FILES_DIR/tf-mirror-downloader.service"
    sed -i "s|/var/lib/tf-mirror|$DATA_DIR|g" "$SERVICE_FILES_DIR/tf-mirror-downloader.service"
    sed -i "s|/opt/tf-mirror|$INSTALL_DIR|g" "$SERVICE_FILES_DIR/tf-mirror-server.service"
    sed -i "s|/var/lib/tf-mirror|$DATA_DIR|g" "$SERVICE_FILES_DIR/tf-mirror-server.service"

    # Reload systemd
    systemctl daemon-reload

    log_info "Installed systemd services"
}

# Configure firewall (if ufw is available)
configure_firewall() {
    if command -v ufw &> /dev/null; then
        log_step "Configuring firewall..."

        # Allow HTTP traffic on port 8080
        ufw allow 8080/tcp comment 'Terraform Mirror HTTP'

        log_info "Added firewall rule for port 8080"
    else
        log_warn "ufw not found, skipping firewall configuration"
        log_warn "Please ensure port 8080 is accessible if needed"
    fi
}

# Create configuration examples
create_config_examples() {
    log_step "Creating configuration examples..."

    # Create environment file
    cat > "$INSTALL_DIR/config/downloader.env" << EOF
# Terraform Mirror Downloader Configuration
# Uncomment and modify as needed

# CHECK_PERIOD=6
# PROXY=http://proxy.example.com:8080
# DEBUG=1
EOF

    cat > "$INSTALL_DIR/config/server.env" << EOF
# Terraform Mirror Server Configuration
# Uncomment and modify as needed

# LISTEN_HOST=0.0.0.0
# LISTEN_PORT=8080
# HOSTNAME=localhost
# ENABLE_TLS=false
# TLS_CRT=/etc/ssl/certs/terraform-mirror.crt
# TLS_KEY=/etc/ssl/private/terraform-mirror.key
# DEBUG=1
EOF

    # Create example .terraformrc
    mkdir -p "$INSTALL_DIR/config/terraform"
    cp ./examples/terraform/.terraformrc "$INSTALL_DIR/config/terraform/"

    chown -R "$USER:$GROUP" "$INSTALL_DIR/config"

    log_info "Created configuration examples in $INSTALL_DIR/config/"
}

# Create log rotation
setup_log_rotation() {
    log_step "Setting up log rotation..."

    cat > /etc/logrotate.d/tf-mirror << EOF
$INSTALL_DIR/logs/*.log {
    daily
    missingok
    rotate 30
    compress
    delaycompress
    notifempty
    create 644 $USER $GROUP
    postrotate
        systemctl reload-or-restart tf-mirror-downloader
        systemctl reload-or-restart tf-mirror-server
    endscript
}
EOF

    log_info "Configured log rotation"
}

# Enable and start services
start_services() {
    log_step "Enabling and starting services..."

    # Enable services
    systemctl enable tf-mirror-downloader.service
    systemctl enable tf-mirror-server.service

    log_info "Enabled services for automatic startup"

    # Ask user if they want to start services now
    echo
    read -p "Do you want to start the services now? (Y/n): " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Nn]$ ]]; then
        systemctl start tf-mirror-downloader.service
        sleep 5  # Give downloader time to start
        systemctl start tf-mirror-server.service

        log_info "Started services"

        # Show service status
        echo
        log_info "Service status:"
        systemctl --no-pager status tf-mirror-downloader.service
        echo
        systemctl --no-pager status tf-mirror-server.service
    else
        log_info "Services not started. You can start them later with:"
        log_info "  systemctl start tf-mirror-downloader"
        log_info "  systemctl start tf-mirror-server"
    fi
}

# Show usage instructions
show_usage() {
    log_step "Installation complete!"
    echo
    log_info "Installation Summary:"
    log_info "  Install directory: $INSTALL_DIR"
    log_info "  Data directory: $DATA_DIR"
    log_info "  User/Group: $USER:$GROUP"
    log_info "  Services: tf-mirror-downloader, tf-mirror-server"
    log_info "  Binary: tf-mirror (unified)"
    echo
    log_info "Useful commands:"
    log_info "  Check service status:"
    log_info "    systemctl status tf-mirror-downloader"
    log_info "    systemctl status tf-mirror-server"
    echo
    log_info "  View logs:"
    log_info "    journalctl -u tf-mirror-downloader -f"
    log_info "    journalctl -u tf-mirror-server -f"
    echo
    log_info "  Configuration files:"
    log_info "    $INSTALL_DIR/config/downloader.env"
    log_info "    $INSTALL_DIR/config/terraform/.terraformrc"
    echo
    log_info "  Test the mirror:"
    log_info "    curl http://localhost:8080/.well-known/terraform.json"
    log_info "    curl http://localhost:8080/v1/providers"
    echo
    log_info "  Configure Terraform to use the mirror:"
    log_info "    cp $INSTALL_DIR/config/terraform/.terraformrc ~/.terraformrc"
    log_info ""
    log_info "  Run modes:"
    log_info "    $INSTALL_DIR/bin/tf-mirror --mode downloader --download-path $DATA_DIR"
    log_info "    $INSTALL_DIR/bin/tf-mirror --mode server --data-path $DATA_DIR"
    echo
    log_warn "Note: The downloader will take some time to download all providers."
    log_warn "Monitor the logs to see progress: journalctl -u tf-mirror-downloader -f"
}

# Uninstall function
uninstall() {
    log_step "Uninstalling Terraform Mirror..."

    # Stop and disable services
    systemctl stop tf-mirror-downloader.service tf-mirror-server.service 2>/dev/null || true
    systemctl disable tf-mirror-downloader.service tf-mirror-server.service 2>/dev/null || true

    # Remove service files
    rm -f "$SERVICE_FILES_DIR/tf-mirror-downloader.service"
    rm -f "$SERVICE_FILES_DIR/tf-mirror-server.service"
    systemctl daemon-reload

    # Remove directories (ask user about data)
    echo
    read -p "Do you want to remove the data directory $DATA_DIR? (y/N): " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        rm -rf "$DATA_DIR"
        log_info "Removed data directory"
    else
        log_info "Kept data directory: $DATA_DIR"
    fi

    rm -rf "$INSTALL_DIR"

    # Remove user and group
    if getent passwd "$USER" > /dev/null 2>&1; then
        userdel "$USER"
        log_info "Removed user: $USER"
    fi

    if getent group "$GROUP" > /dev/null 2>&1; then
        groupdel "$GROUP"
        log_info "Removed group: $GROUP"
    fi

    # Remove log rotation
    rm -f /etc/logrotate.d/tf-mirror

    log_info "Uninstallation complete"
}

# Main function
main() {
    echo "Terraform Registry Mirror Deployment Script"
    echo "==========================================="
    echo

    case "${1:-install}" in
        install)
            check_root
            check_requirements
            create_user
            create_directories
            install_binaries
            install_services
            configure_firewall
            create_config_examples
            setup_log_rotation
            start_services
            show_usage
            ;;
        uninstall)
            check_root
            uninstall
            ;;
        *)
            echo "Usage: $0 [install|uninstall]"
            echo
            echo "Commands:"
            echo "  install     - Install terraform mirror (default)"
            echo "  uninstall   - Remove terraform mirror"
            exit 1
            ;;
    esac
}

# Run main function
main "$@"
