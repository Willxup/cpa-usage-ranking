#!/usr/bin/env python3
"""Fail closed unless every required reusable CI job succeeded."""

import os
import sys


REQUIRED = ("BACKEND_RESULT", "FRONTEND_RESULT", "CONTAINER_RESULT")


def main():
    for name in REQUIRED:
        if os.environ.get(name) != "success":
            print(f"Required check {name} did not succeed", file=sys.stderr)
            return 1
    print("All required checks succeeded")
    return 0


if __name__ == "__main__":
    sys.exit(main())
