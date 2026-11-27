#!/bin/sh
set -eu

mkdir -p /home/hop/.ssh
if [ -f /keys/id_ed25519.pub ]; then
  cp /keys/id_ed25519.pub /home/hop/.ssh/authorized_keys
  chmod 0600 /home/hop/.ssh/authorized_keys
fi
chown -R hop:hop /home/hop/.ssh
chmod 0700 /home/hop/.ssh

ssh-keygen -A
exec /usr/sbin/sshd -D -e \
  -p 2222 \
  -o PermitRootLogin=no \
  -o PasswordAuthentication=no \
  -o PubkeyAuthentication=yes \
  -o AllowUsers=hop \
  -o PidFile=/run/sshd.pid
