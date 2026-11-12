#!/usr/bin/env python3
"""Validate a GHCR publication and emit only safe, fully qualified image tags.

Run from a full checkout made with fetch-depth: 0. In particular, the checkout
must contain all tags and the current origin/<default branch> remote-tracking
ref. No network access or registry credentials are required here.
"""

import os
from pathlib import Path
import re
import subprocess
import sys


STABLE = re.compile(r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\Z")
SHA = re.compile(r"[0-9a-f]{40}\Z")
OWNER = re.compile(r"[a-z0-9]+(?:-[a-z0-9]+)*\Z")
REPO = re.compile(r"[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*\Z")
DEV_TAG = re.compile(r"[a-z0-9][a-z0-9._-]{0,123}\Z")
VERSION_ALIAS = re.compile(r"v?[0-9]+(?:\.[0-9]+){0,2}\Z")


class PolicyError(Exception):
    pass


def require(condition, message):
    if not condition:
        raise PolicyError(message)


def git(*args):
    result = subprocess.run(["git", *args], capture_output=True, text=True, check=False)
    require(result.returncode == 0, "Git reference or history validation failed")
    return result.stdout.strip()


def branch_ref(name):
    require(bool(name) and len(name) <= 255, "Invalid branch name")
    # The full ref is passed as an argument, never interpreted by a shell.
    ref = "refs/heads/" + name
    git("check-ref-format", ref)
    return ref


def resolve(env):
    repository = env.get("GITHUB_REPOSITORY", "").lower()
    parts = repository.split("/")
    require(len(parts) == 2 and len(parts[0]) <= 39 and len(parts[1]) <= 100 and
            bool(OWNER.fullmatch(parts[0])) and bool(REPO.fullmatch(parts[1])),
            "Invalid GHCR repository name")
    image = "ghcr.io/" + repository

    sha = env.get("GITHUB_SHA", "")
    require(bool(SHA.fullmatch(sha)), "Expected a full lowercase commit SHA")
    event_commit = git("rev-parse", "--verify", sha + "^{commit}")
    require(git("rev-parse", "--verify", "HEAD^{commit}") == event_commit,
            "Checkout does not match the event commit")

    default_branch = env.get("DEFAULT_BRANCH", "")
    branch_ref(default_branch)
    ref, name = env.get("GITHUB_REF", ""), env.get("GITHUB_REF_NAME", "")
    event = env.get("GITHUB_EVENT_NAME", "")
    dev_tag = env.get("DEV_TAG", "")

    if event == "push":
        require(not dev_tag and len(name) <= 128 and bool(STABLE.fullmatch(name)) and
                ref == "refs/tags/" + name,
                "Release requires a stable vMAJOR.MINOR.PATCH tag")
        tag_commit = git("rev-parse", "--verify", ref + "^{commit}")
        require(tag_commit == event_commit, "The tag no longer matches the event commit")
        default_ref = "refs/remotes/origin/" + default_branch
        git("rev-parse", "--verify", default_ref + "^{commit}")
        git("merge-base", "--is-ancestor", tag_commit, default_ref)
        # No moving latest/major/minor aliases: a backfill cannot roll them back.
        tags = [name, "sha-" + event_commit]
    elif event == "workflow_dispatch":
        require(ref == branch_ref(name) and name != default_branch,
                "Development publishing requires a non-default branch")
        tags = ["dev-sha-" + event_commit]
        if dev_tag:
            require(bool(DEV_TAG.fullmatch(dev_tag)), "Invalid development tag")
            require(dev_tag not in {"latest", "dev", "sha"} and
                    not dev_tag.startswith("sha-") and
                    not VERSION_ALIAS.fullmatch(dev_tag),
                    "Development tag is reserved")
            tags.append("dev-" + dev_tag)
    else:
        raise PolicyError("Unsupported publication event")

    return image, [image + ":" + tag for tag in tags]


def main():
    try:
        image, tags = resolve(os.environ)
        output = os.environ.get("GITHUB_OUTPUT", "")
        require(bool(output), "GITHUB_OUTPUT is required")
        # All output values have been restricted to safe single-line characters.
        with Path(output).open("a", encoding="utf-8") as file:
            file.write("image=" + image + "\ntags<<GHCR_TAGS\n")
            file.write("\n".join(tags) + "\nGHCR_TAGS\n")
        print("Validated GHCR publication policy")
    except (PolicyError, OSError) as error:
        print("GHCR policy rejected publication: " + str(error), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
