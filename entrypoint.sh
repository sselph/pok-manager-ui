#!/bin/sh

# Default UID/GID for pokuser
PUID=${PUID:-7777}
PGID=${PGID:-7777}

echo "Configuring pokuser with UID $PUID and GID $PGID..."

# Create group if it doesn't exist
if ! getent group pokuser >/dev/null; then
    addgroup -g "$PGID" pokuser
fi

# Create user if it doesn't exist
if ! getent passwd pokuser >/dev/null; then
    adduser -u "$PUID" -G pokuser -D -s /bin/sh pokuser
fi

# Check if docker socket exists and resolve its GID dynamically
if [ -S /var/run/docker.sock ]; then
    DOCKER_GID=$(stat -c '%g' /var/run/docker.sock)
    echo "Detected host Docker socket GID: $DOCKER_GID"
    
    # Check if a group with this GID already exists in the container's /etc/group
    EXISTING_GROUP=$(awk -F: -v gid="$DOCKER_GID" '$3 == gid {print $1}' /etc/group)
    
    if [ -n "$EXISTING_GROUP" ]; then
        echo "Group with GID $DOCKER_GID already exists: $EXISTING_GROUP"
        addgroup pokuser "$EXISTING_GROUP"
    else
        echo "Creating new group host-docker with GID $DOCKER_GID"
        addgroup -g "$DOCKER_GID" host-docker
        addgroup pokuser host-docker
    fi
fi

# Ensure workspace and configs inside the container are owned by pokuser
chown -R pokuser:pokuser /app

# Run the backend binary as pokuser using su-exec (drops root privilege)
echo "Starting POK Manager as user pokuser ($PUID:$PGID)..."
exec su-exec pokuser /app/pok-manager "$@"
