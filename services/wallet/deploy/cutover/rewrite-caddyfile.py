#!/usr/bin/env python3
"""Point one Caddyfile site block's reverse_proxy directive at a new upstream.

usage: rewrite-caddyfile.py <caddyfile> <site-label> <directive-file> <output>

The site block is the top-level block whose address line is exactly
<site-label>. Inside it, the one reverse_proxy directive at the block's own
depth (or the marked directive a previous run wrote) is replaced by the
contents of <directive-file>, framed by BEGIN and END marker comments and
indented like the line it replaces. Every other line of the file is copied
unchanged. Exit 0 when the output differs from the input, 3 when it is
identical, 2 on any refusal.
"""

import sys

BEGIN = "# BEGIN paxeer-wallet-cutover"
END = "# END paxeer-wallet-cutover"


def fail(message):
    print("rewrite-caddyfile: " + message, file=sys.stderr)
    sys.exit(2)


def code_part(line):
    """The line without a trailing comment, following Caddy's rule that # starts
    a comment at the start of a line or after whitespace, outside quotes."""
    quoted = False
    for i, ch in enumerate(line):
        if ch == '"' and (i == 0 or line[i - 1] != "\\"):
            quoted = not quoted
        elif ch == "#" and not quoted and (i == 0 or line[i - 1] in " \t"):
            return line[:i]
    return line


def depth_change(line):
    code = code_part(line)
    return code.count("{") - code.count("}")


def main():
    if len(sys.argv) != 5:
        fail("usage: rewrite-caddyfile.py <caddyfile> <site-label> <directive-file> <output>")
    source, label, directive_file, output = sys.argv[1:]
    with open(source, encoding="utf-8") as f:
        text = f.read()
    with open(directive_file, encoding="utf-8") as f:
        directive = [l for l in f.read().splitlines() if l.strip()]
    if not directive or not directive[0].startswith("reverse_proxy "):
        fail("the directive file must hold one reverse_proxy directive")
    lines = text.splitlines()

    starts = []
    depth = 0
    for i, line in enumerate(lines):
        if depth == 0:
            code = code_part(line).strip()
            if code.endswith("{") and code[:-1].strip() == label:
                starts.append(i)
        depth += depth_change(line)
        if depth < 0:
            fail("unbalanced braces at line %d" % (i + 1))
    if depth != 0:
        fail("unbalanced braces at the end of the file")
    if len(starts) != 1:
        fail("expected exactly one site block with the address %r, found %d" % (label, len(starts)))
    start = starts[0]
    depth = depth_change(lines[start])
    end = start
    while depth > 0:
        end += 1
        depth += depth_change(lines[end])

    body = list(range(start + 1, end))
    marked = [i for i in body if lines[i].strip() in (BEGIN, END)]
    if marked:
        if len(marked) != 2 or lines[marked[0]].strip() != BEGIN or lines[marked[1]].strip() != END:
            fail("the site block holds a broken cutover marker pair")
        first, last = marked
    else:
        found = []
        depth = 1
        for i in body:
            code = code_part(lines[i]).strip()
            if depth == 1 and (code == "reverse_proxy" or code.startswith("reverse_proxy ")):
                found.append(i)
            depth += depth_change(lines[i])
        if len(found) != 1:
            fail("expected exactly one reverse_proxy directive in the site block, found %d" % len(found))
        first = found[0]
        last = first
        depth = depth_change(lines[first])
        while depth > 0:
            last += 1
            depth += depth_change(lines[last])

    indent = lines[first][: len(lines[first]) - len(lines[first].lstrip())]
    replacement = [indent + BEGIN] + [indent + l for l in directive] + [indent + END]
    result = lines[:first] + replacement + lines[last + 1 :]
    rendered = "\n".join(result) + ("\n" if text.endswith("\n") else "")
    with open(output, "w", encoding="utf-8") as f:
        f.write(rendered)
    sys.exit(0 if rendered != text else 3)


if __name__ == "__main__":
    main()
