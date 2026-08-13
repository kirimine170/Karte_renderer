# Release policy

Karte Renderer uses [Semantic Versioning](https://semver.org/) for the Go
module and the `karte-renderer` command. The Git tag is the authoritative
release version; versions in Node package metadata describe only the pinned
development tool package and are not Renderer release versions.

## Versioning

Before `v1.0.0`, compatibility is expressed as follows:

- `PATCH` (`v0.1.0` to `v0.1.1`) fixes bugs or changes documentation and does
  not intentionally break the public Go API or CLI.
- `MINOR` (`v0.1.1` to `v0.2.0`) adds functionality and may contain an
  intentional incompatible change. Such a change must be called out under a
  `Changed` or `Removed` heading in the changelog.
- `MAJOR` starts with `v1.0.0`. After that release, ordinary SemVer rules
  apply: incompatible public API or CLI changes increment `MAJOR`, compatible
  features increment `MINOR`, and compatible fixes increment `PATCH`.

Release candidates may use `vMAJOR.MINOR.PATCH-rc.N`. They are published in
the same way as stable releases but GitHub marks them as prereleases. Karte
must not depend on a prerelease unless it is deliberately testing one.

Repository commits that have not been released remain available as Go
pseudo-versions, but Karte's main branch must normally depend on a release
tag. The module path remains `github.com/kirimine170/KarteRenderer`; the
repository name does not determine the import path.

## Tags and artifacts

Release tags are annotated tags named `vMAJOR.MINOR.PATCH`, optionally with a
SemVer prerelease suffix such as `v0.2.0-rc.1`. Move, reuse, or delete a
published tag only to recover from an accidental release that contains no
usable artifacts and has not been consumed. Otherwise publish a new patch (or
prerelease) version.

The release workflow publishes the command for these targets:

| Target | Archive |
| --- | --- |
| Linux amd64 | `karte-renderer_VERSION_linux_amd64.tar.gz` |
| Linux arm64 | `karte-renderer_VERSION_linux_arm64.tar.gz` |
| macOS amd64 | `karte-renderer_VERSION_darwin_amd64.tar.gz` |
| macOS arm64 | `karte-renderer_VERSION_darwin_arm64.tar.gz` |
| Windows amd64 | `karte-renderer_VERSION_windows_amd64.zip` |
| Windows arm64 | `karte-renderer_VERSION_windows_arm64.zip` |

`VERSION` includes the leading `v`. Each archive contains the executable,
`README.md`, and `LICENSE` when the repository has one. `checksums.txt` lists
SHA-256 checksums for all archives. The executable still needs the runtime
dependencies documented in `README.md`; in particular, Marp conversion needs
Node.js and the pinned npm dependencies.

Go consumers do not need an archive. They select the same release through the
module tag, for example:

```sh
go get github.com/kirimine170/KarteRenderer@v0.1.0
go mod tidy
go test ./...
```

## Changelog policy

`CHANGELOG.md` follows Keep a Changelog headings. User-visible changes are
added to `Unreleased` in the pull request that introduces them. Internal-only
changes may be omitted. At release time, move the entries into a heading with
the version and ISO date, and add an empty `Unreleased` section. Breaking
changes must be explicit.

## Release procedure

1. Confirm `main` is green and contains exactly the intended release changes.
2. Choose the version using the rules above. Update `CHANGELOG.md`, including
   the release date and any breaking or migration notes, in a pull request.
3. Merge that pull request and verify locally on the resulting `main` commit:

   ```sh
   go test ./...
   go vet ./...
   npm ci
   npm test
   ```

4. Create and push an annotated tag on that exact commit:

   ```sh
   git switch main
   git pull --ff-only
   git tag -a v0.1.0 -m "Karte Renderer v0.1.0"
   git push origin v0.1.0
   ```

5. The `Release` workflow validates the tag, reruns tests, builds archives and
   checksums, and creates the GitHub Release. Verify its artifacts and generated
   notes.
6. In Karte, replace the pseudo-version with the released tag, run `go mod
   tidy` and the relevant tests, and merge that dependency update separately.

Version selection, changelog curation, tagging, release verification, and the
Karte dependency update are human decisions. Validation, binary packaging,
checksums, and GitHub Release creation are automated.

## Failure and rollback

If validation or packaging fails, the workflow does not create a release.
Keep the tag while diagnosing if no corrected commit is required, then rerun
the failed job. If the tagged commit itself must change and the tag has not
been consumed, delete the remote and local tag, fix `main`, and recreate it.
Record this exceptional action in the issue or pull request.

If GitHub Release creation fails after archives were built, rerun the workflow;
the command uses the same tag and updates the existing release when necessary.
If a published release has been consumed or has a functional defect, do not
move its tag. Document the problem, publish a new patch release, and update
Karte to that version. Roll Karte back to its previous known-good release tag
when an immediate dependency rollback is required.
