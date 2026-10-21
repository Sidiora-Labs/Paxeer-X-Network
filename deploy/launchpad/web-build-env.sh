# Sourced before the web build: an omitted or empty NEXT_PUBLIC_*/WEB_* build arg must stay unset so the app's fallbacks apply.
for _web_build_var in $(env | grep -E '^(NEXT_PUBLIC|WEB)_[A-Za-z0-9_]*=$' | cut -d= -f1); do
  unset "$_web_build_var"
done
unset _web_build_var
