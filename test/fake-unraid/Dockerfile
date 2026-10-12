# A stand-in Unraid server for end-to-end tests: Docker (dind) reachable
# over password SSH, with Unraid's share and appdata paths.
FROM docker:28-dind
RUN apk add --no-cache openssh iproute2 && ssh-keygen -A \
 && sed -i 's/^#\?PermitRootLogin.*/PermitRootLogin yes/; s/^#\?PasswordAuthentication.*/PasswordAuthentication yes/' /etc/ssh/sshd_config \
 && mkdir -p /mnt/user /mnt/cache/appdata
COPY start.sh /start.sh
ENTRYPOINT ["/bin/sh", "/start.sh"]
