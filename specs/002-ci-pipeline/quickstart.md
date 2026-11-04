# Quickstart: validating the CI pipeline

Prerequisites: Docker, `make`, `git`, and push access to `hermeto/tiny-s3`. Steps 4–7 need the workflow on
GitHub; step 8 needs repository admin.

## 1. Local behaviour unchanged

```sh
make vet test race cover          # all exit 0, caches in named volumes as before
docker volume ls | grep tiny-s3   # tiny-s3-gomod, tiny-s3-gobuild still used
```

## 2. Cache override works locally

```sh
make test GOMOD_CACHE="$PWD/.tmp/gomod" GOBUILD_CACHE="$PWD/.tmp/gobuild"
ls .tmp/gobuild | head            # build cache written to the bind mount
rm -rf .tmp                       # may need sudo on Linux hosts: files are root-owned
```

See [contracts/make.md](contracts/make.md).

## 3. Lint the workflow

```sh
docker run --rm -v "$PWD":/repo -w /repo \
  rhysd/actionlint:1.7.12@sha256:b1934ee5f1c509618f2508e6eb47ee0d3520686341fec936f3b79331f9315667 -color
```

Expected: no output, exit 0.

## 4. Green run on a branch (US1, SC-001, SC-003)

Push the feature branch and open a pull request to `main`. Expected: five checks per event, named as in
[contracts/ci-checks.md](contracts/ci-checks.md), all green; `publish` absent/skipped. Note the wall-clock time
of a second run of the same commit (re-run all jobs): under 10 minutes and faster than the first, with
"Cache restored" in the gate logs.

## 5. Each gate fails on its own (US1 scenarios 3–6, SC-002, SC-006)

On throwaway branches from the feature branch, one change each, confirm locally first and then in CI:

| Seeded change | Local command fails | Failing CI checks |
|---------------|--------------------|-------------------|
| A function that takes a `sync.Mutex` by value (`copylocks`, outside the vet subset `go test` runs) | `make vet` | `make vet` |
| `t.Fatal("seeded")` in one test | `make test` | `make test`, `make race`, `make cover` (all run the tests) |
| Unsynchronized write to a shared variable from two goroutines in a test | `make race` | `make race` |
| New exported function under `internal/` with no test | `make cover` | `make cover` |
| `COPY missing-file /` in the Dockerfile | `docker build .` | `docker build` |

Expected: CI and local agree on every row; the checks not listed still run and pass; the failing check's log
shows the `make` command. Delete the branches afterwards.

## 6. Cancellation (US2, SC-004)

```sh
git commit --allow-empty -m "chore: ci cancel test 1" && git push
sleep 15
git commit --allow-empty -m "chore: ci cancel test 2" && git push
```

Expected: the first run shows `cancelled` within a minute of the second push; runs on other branches are
untouched.

## 7. Publish on main (US3, SC-005)

After merging to `main`:

```sh
sha=$(git rev-parse origin/main)
docker pull ghcr.io/hermeto/tiny-s3:"$sha"
docker pull ghcr.io/hermeto/tiny-s3:latest
docker image inspect --format '{{index .RepoDigests 0}}' ghcr.io/hermeto/tiny-s3:"$sha" ghcr.io/hermeto/tiny-s3:latest
```

Expected: both resolve to the same digest; `docker run --rm -p 9000:9000 ghcr.io/hermeto/tiny-s3:latest` answers
`curl -s localhost:9000/` with the `NotImplemented` XML error. If the package is private, `docker login ghcr.io`
first. Confirm no package version was created for the earlier branch and pull request runs.

Once `main` has at least two published commits, re-run all jobs of the older successful `main` run. Expected:
`publish` passes, its log says `not tagging <sha> as latest`, and `latest` still resolves to the newest commit's
digest (FR-011).

## 8. Required checks (US4, SC-007)

Follow the README's "Protecting main" steps. Then open a pull request from the step 5 `make test` branch.
Expected: merge is blocked until the check passes; a direct `git push origin main` is rejected.
