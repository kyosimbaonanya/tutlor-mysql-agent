#!/sbin/openrc-run
name=$RC_SVCNAME
description="Tutlor MySQL Agent"
command="/usr/local/bin/agent/agent.sh"
command_user="tutlor:tutlor"
command_background="yes"
pidfile="/var/run/tutlor-mysql-agent.pid"
directory="$(dirname $pidfile)"
directory="$(dirname $pidfile)"

 depend() {
    after net
    after mariadb
    after mysql
}

start_pre() {
  if [ ! -d "$directory" ]; then
        echo "Creating directory: $directory"
        mkdir -p "$directory"
  fi
  ip link set lo up
  hostname tutlor
  echo "127.0.0.1 localhost" >> /etc/hosts; echo "::1 localhost" >> /etc/hosts
  echo "nameserver 1.1.1.1" >>/etc/resolv.conf
  # sed -i "s|.*skip-networking.*|# skip-networking|g" /etc/my.cnf.d/mariadb-server.cnf
  rc-
  chmod 0755 "$directory"
  chown "$command_user" "$directory"
}

stop_post() {
    rm -f "$pidfile"
}

