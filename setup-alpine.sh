#!/bin/sh
apk add  openrc
apk add  util-linux

ln -s agetty /etc/init.d/agetty.ttyS0
echo ttyS0 >/etc/securetty
rc-update add agetty.ttyS0 default

echo "root:root" | chpasswd

echo "nameserver 1.1.1.1" >>/etc/resolv.conf

# Create a new user called "tutlor"
addgroup -g 1000 -S tutlor && adduser -u 1000 -S tutlor -G tutlor
# mysql
addgroup -S mysql && adduser -S mysql -G mysql && apk add mysql mysql-client

rc-update add devfs boot
rc-update add procfs boot
rc-update add sysfs boot


mkdir -p /var/lib/mysql /var/run/mysqld &&  mkdir /run/openrc && touch /run/openrc/softlevel && rc-service mariadb setup && mkdir -p /run/mysqld && chown -R mysql:mysql /run/mysqld && chown -R mysql:mysql /var/lib/mysql 
mysql_install_db --user=mysql --ldata=/var/lib/mysql > /dev/null
/usr/bin/mysqld --user=mysql --bootstrap --verbose=0  < mktemp

# sed -i "s|.*skip-networking.*|skip-networking|g" /etc/my.cnf.d/mariadb-server.cnf
sed -i "s|.*skip-networking.*|# skip-networking|g" /etc/my.cnf.d/mariadb-server.cnf

rm -f mktemp

rc-status

rc-update add mariadb default
rc-update add agent boot

hostname tutlor

echo -e "[client]\nuser=root\npassword=root" | tee -a /etc/my.cnf

ip link set lo up