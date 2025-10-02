
load_env_noclobber() {
  while IFS='=' read -r key val; do
    # skip blank lines and comments
    [ -z "$key" ] && continue
    case "$key" in \#*) continue ;; esac
    # only skip if variable is non-empty
    if [ -z "${!key+x}" ] || [ -z "${!key}" ]; then
      export "$key=$val"
    fi
  done < "$1"
}
