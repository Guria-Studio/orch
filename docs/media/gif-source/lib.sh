export PS1='$ ' ORCH_NO_UPDATE_CHECK=1 COLUMNS=100 LINES=30 TERM=xterm-256color
type_run() {  # print "$ cmd" as if typed, then run it for real
  printf '$ '; s="$1"; i=0
  while [ $i -lt ${#s} ]; do printf '%s' "${s:$i:1}"; sleep 0.035; i=$((i+1)); done
  sleep 0.3; printf '\n'; eval "$1"; sleep "${2:-1}"
}
