#!/usr/bin/env python3
import argparse
import collections
import os
import stat
import sys


class OwnershipError(Exception):
    pass


DIRECTORY_FLAGS = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC


def open_repository(path):
    if not os.path.isabs(path) or ".." in path.split(os.sep):
        raise OwnershipError("repository must be an absolute path without parent traversal")
    fd = os.open(os.sep, DIRECTORY_FLAGS)
    try:
        for component in path.split(os.sep):
            if not component:
                continue
            next_fd = os.open(component, DIRECTORY_FLAGS, dir_fd=fd)
            os.close(fd)
            fd = next_fd
        return fd
    except BaseException:
        os.close(fd)
        raise


def output_components(repository, output):
    if ".." in output.split(os.sep):
        raise OwnershipError("output parent traversal is refused")
    absolute = os.path.abspath(output)
    repository = os.path.normpath(repository)
    if absolute == repository or os.path.commonpath([repository, absolute]) != repository:
        raise OwnershipError("output must be strictly inside the repository")
    components = os.path.relpath(absolute, repository).split(os.sep)
    if any(component == ".git" or component.startswith(".env") for component in components):
        raise OwnershipError("repository metadata and environment paths are refused")
    return components


def open_output(repository_fd, components):
    fd = os.dup(repository_fd)
    try:
        for component in components:
            try:
                next_fd = os.open(component, DIRECTORY_FLAGS, dir_fd=fd)
            except FileNotFoundError:
                os.close(fd)
                return None
            os.close(fd)
            fd = next_fd
        return fd
    except BaseException:
        os.close(fd)
        raise


def validate_tree(fd, links):
    for name in os.listdir(fd):
        if name.startswith(".env"):
            continue
        info = os.stat(name, dir_fd=fd, follow_symlinks=False)
        if stat.S_ISLNK(info.st_mode):
            continue
        if stat.S_ISDIR(info.st_mode):
            child = os.open(name, DIRECTORY_FLAGS, dir_fd=fd)
            try:
                validate_tree(child, links)
            finally:
                os.close(child)
        elif stat.S_ISREG(info.st_mode):
            identity = (info.st_dev, info.st_ino)
            links[identity][0] += 1
            links[identity][1] = info.st_nlink
        else:
            raise OwnershipError("output contains a special file")


def restore(fd, owner):
    for name in os.listdir(fd):
        if name.startswith(".env"):
            continue
        info = os.stat(name, dir_fd=fd, follow_symlinks=False)
        if stat.S_ISLNK(info.st_mode):
            continue
        if stat.S_ISDIR(info.st_mode):
            flags = DIRECTORY_FLAGS
        elif stat.S_ISREG(info.st_mode):
            flags = os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC | os.O_NONBLOCK
        else:
            raise OwnershipError("output contains a special file")
        child = os.open(name, flags, dir_fd=fd)
        try:
            opened = os.fstat(child)
            if (opened.st_dev, opened.st_ino) != (info.st_dev, info.st_ino):
                raise OwnershipError("output changed during ownership restoration")
            if stat.S_ISDIR(opened.st_mode):
                restore(child, owner)
            else:
                os.fchown(child, *owner)
                os.fchmod(child, stat.S_IMODE(opened.st_mode))
        finally:
            os.close(child)
    mode = stat.S_IMODE(os.fstat(fd).st_mode)
    os.fchown(fd, *owner)
    os.fchmod(fd, mode)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--repo", required=True)
    parser.add_argument("--owner")
    parser.add_argument("--output", action="append", default=[])
    parser.add_argument("--capture-owner", action="store_true")
    parser.add_argument("--validate-only", action="store_true")
    args = parser.parse_args()
    repository_fd = open_repository(args.repo)
    outputs = []
    try:
        info = os.fstat(repository_fd)
        owner = (info.st_uid, info.st_gid)
        if args.capture_owner:
            if args.owner or args.output or args.validate_only:
                raise OwnershipError("owner capture cannot restore outputs")
            print(f"{owner[0]}:{owner[1]}")
            return
        if args.owner != f"{owner[0]}:{owner[1]}":
            raise OwnershipError("captured owner does not match the actual repository")
        if not args.output:
            raise OwnershipError("at least one exact output is required")
        paths = sorted({tuple(output_components(args.repo, output)) for output in args.output}, key=len)
        selected = []
        for components in paths:
            if any(components[:len(parent)] == parent for parent in selected):
                continue
            selected.append(components)
        links = collections.defaultdict(lambda: [0, 0])
        for components in selected:
            fd = open_output(repository_fd, components)
            if fd is not None:
                outputs.append(fd)
                validate_tree(fd, links)
        if any(count != total for count, total in links.values()):
            raise OwnershipError("a file has hard links outside the admitted output trees")
        if not args.validate_only:
            if os.geteuid() != 0:
                raise OwnershipError("ownership restoration requires root")
            for fd in outputs:
                restore(fd, owner)
    finally:
        for fd in outputs:
            os.close(fd)
        os.close(repository_fd)


if __name__ == "__main__":
    try:
        main()
    except (OwnershipError, OSError) as error:
        print(f"build ownership restoration refused: {error}", file=sys.stderr)
        sys.exit(1)
