#!/bin/bash
set -e

if [ -e /.backup-restored ];
then
  echo
  echo 'Starting MySQL using prepared backup.'
  echo
else
  mkdir -p /var/lib/mysql
  restore-percona-backup $1
  chown -R mysql:mysql /var/lib/mysql
  mysql_install_db --datadir=/var/lib/mysql --user=mysql

  mysqld &
  pid="$!"

  mysql=( mysql --protocol=socket -uroot )
  for i in {30..0}; do
  	if echo 'SELECT 1' | "${mysql[@]}" &> /dev/null; then
      break
    fi
    echo 'MySQL init process in progress...'
    sleep 1
  done
  if [ "$i" = 0 ]; then
    echo >&2 'MySQL init process failed.'
    exit 1
  fi

  echo "GRANT ALL PRIVILEGES ON *.* to root@`%` WITH GRANT OPTION;" | "${mysql[@]}"
  echo "FLUSH PRIVILEGES;" | "${mysql[@]}"

  if ! kill -s TERM "$pid" || ! wait "$pid"; then
    echo >&2 'MySQL init process failed.'
    exit 1
  fi

  touch /.backup-restored

  echo
  echo 'MySQL init process done. Ready for start up.'
  echo
fi

exec "mysqld"