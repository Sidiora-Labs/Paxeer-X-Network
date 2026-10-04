#!/bin/sh
set -eu

build_dir=${1:-build}
source_dirs="include/layerx src"

if find $source_dirs -type f -name '*.[ch]' -print0 | \
    xargs -0 grep -nE '(^|[^[:alnum:]_])(float|double|long[[:space:]]+double)([^[:alnum:]_]|$)|#[[:space:]]*include[[:space:]]*[<\"]math\.h[>\"]|(^|[^[:alnum:]_])(([0-9]+\.[0-9]*|\.[0-9]+)([eE][+-]?[0-9]+)?|[0-9]+[eE][+-]?[0-9]+|0[xX][0-9a-fA-F]+(\.[0-9a-fA-F]*)?[pP][+-]?[0-9]+)[fFlL]?([^[:alnum:]_]|$)'; then
    echo "consensus source contains floating-point syntax" >&2
    exit 1
fi

objects=$(find "$build_dir/obj/src" -type f -name '*.o' 2>/dev/null || true)
if [ -n "$objects" ] && objdump -d $objects | \
    grep -E '[[:space:]](f(add|sub|mul|div|ld|st|com|ucom|sqrt|sin|cos|tan)|v?(add|sub|mul|div|max|min|sqrt|com|ucom|cvt|round|mov)(ss|sd|ps|pd))[[:space:]]'; then
    echo "consensus object contains floating-point instructions" >&2
    exit 1
fi

if [ -n "$objects" ] && nm -u $objects | \
    grep -E '[[:space:]]U[[:space:]]+(acos|asin|atan|atan2|ceil|cos|cosh|exp|fabs|floor|fmod|frexp|ldexp|log|log10|modf|pow|sin|sinh|sqrt|tan|tanh)(f|l)?(@.*)?$'; then
    echo "consensus object imports a libm floating-point symbol" >&2
    exit 1
fi

printf '%s\n' "no floating-point syntax, instructions or libm imports in consensus paths"
