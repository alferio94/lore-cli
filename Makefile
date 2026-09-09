PROFILE ?= .github/release-profiles/development-all-off.json
PRERELEASE_PROFILE_FIXTURE ?= internal/releaseprofile/testdata/valid.json
PRERELEASE_PROFILE_SNAPSHOT ?= .github/release-profiles/prerelease-opencode-e.json
OUTPUT ?=

.PHONY: release-profile-ldflags release-profile-snapshots verify-release-profile-snapshots build-profile
release-profile-ldflags:
	@./scripts/release-profile-ldflags.sh "$(PROFILE)"

release-profile-snapshots:
	@./scripts/release-profile-ldflags.sh "$(PRERELEASE_PROFILE_FIXTURE)" >/dev/null
	@cp "$(PRERELEASE_PROFILE_FIXTURE)" "$(PRERELEASE_PROFILE_SNAPSHOT)"

verify-release-profile-snapshots:
	@./scripts/release-profile-ldflags.sh "$(PRERELEASE_PROFILE_FIXTURE)" >/dev/null
	@cmp -s "$(PRERELEASE_PROFILE_FIXTURE)" "$(PRERELEASE_PROFILE_SNAPSHOT)" || { echo "release profile snapshot drift; run make release-profile-snapshots" >&2; exit 1; }

build-profile:
	@test -n "$(OUTPUT)" || { echo "OUTPUT is required" >&2; exit 2; }
	@ldflags="$$(./scripts/release-profile-ldflags.sh "$(PROFILE)")"; \
		test -n "$$ldflags"; \
		go build -trimpath -ldflags "$$ldflags" -o "$(OUTPUT)" ./cmd/lore
