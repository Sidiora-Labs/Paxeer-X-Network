import { readFileSync } from 'node:fs';
import { spawnSync } from 'node:child_process';
import { pathToFileURL } from 'node:url';

/** Package visibility this SDK publishes with, from `publish.npm.access`. */
const ACCESS = 'public';

/**
 * Dist-tag a stable release lands on, from `publish.npm.tag`. `null` leaves npm on its own default,
 * `latest`. A prerelease derives its own tag below and never reads this.
 */
const STABLE_DIST_TAG = null;

/** Reads the package identity once so every registry operation uses the same metadata. */
export const readPackageMetadata = () => {
  const packageJson = JSON.parse(readFileSync(new URL('../package.json', import.meta.url), 'utf8'));
  if (typeof packageJson.name !== 'string' || packageJson.name.length === 0) {
    throw new Error('Missing package name in package.json');
  }
  if (typeof packageJson.version !== 'string' || packageJson.version.length === 0) {
    throw new Error('Missing package version in package.json');
  }
  return { name: packageJson.name, version: packageJson.version };
};

/** Derives an npm dist-tag from a semver prerelease while leaving other npm-handled versions unchanged. */
export const npmDistTagForVersion = (version) => {
  // Calendar versions such as `2026-02-23` use hyphens without denoting a semver prerelease.
  // Only a numeric three-part core followed by `-` is interpreted as a prerelease here; npm
  // remains the source of truth for whether the complete package version is publishable.
  const prerelease = /^\d+\.\d+\.\d+-([^+]+)(?:\+.*)?$/.exec(version)?.[1];
  if (!prerelease) return undefined;
  const candidate = prerelease.split('.')[0];
  return /^[a-z][a-z0-9-]*$/.test(candidate) ? candidate : 'next';
};

/** Runs one npm command with inherited CI credentials and output. */
const runNpm = (args, stdio = 'inherit') => spawnSync('npm', args, { stdio });

/** Publishes the repository package once, applying a derived prerelease dist-tag when needed. */
export const publishNpm = (runner = runNpm, metadata = readPackageMetadata()) => {
  const { name, version } = metadata;
  const packageSpec = `${name}@${version}`;
  if (runner(['view', packageSpec, 'version'], 'ignore').status === 0) {
    console.log(`${packageSpec} already published to npm, skipping`);
    return;
  }

  // A prerelease's own tag wins over the configured one: `publish.npm.tag` names where *stable*
  // releases land, so honouring it for a prerelease would point that tag at a prerelease build.
  //
  // A derived tag naming a stable line is suffixed rather than used as-is, because the two collide:
  // `npmDistTagForVersion` falls back to `next` for any prerelease identifier it cannot use as a
  // tag, so on a package configured with `tag: 'next'` a `1.3.0-RC.1` would otherwise publish over
  // the stable line it names.
  //
  // Both lines are protected, not just the configured one. `latest` is what a bare `npm install`
  // resolves whether or not `publish.npm.tag` moves stable releases elsewhere, so a `1.0.0-latest.1`
  // on a package configured with `tag: 'next'` must not take it either.
  const derived = npmDistTagForVersion(version);
  const stableTags = [STABLE_DIST_TAG ?? 'latest', 'latest'];
  const tag =
    derived === undefined
      ? STABLE_DIST_TAG
      : stableTags.includes(derived)
        ? `${derived}-prerelease`
        : derived;
  const args = ['publish', '--access', ACCESS, ...(tag ? ['--tag', tag] : [])];
  console.log(
    tag ? `Publishing ${packageSpec} to npm with dist-tag ${tag}` : `Publishing ${packageSpec} to npm`,
  );
  const result = runner(args);
  if (result.error) throw result.error;
  if (result.status !== 0) throw new Error(`npm publish failed with exit code ${result.status ?? 'unknown'}`);
};

/** Runs publication only when Node executes this file directly, keeping its helpers testable. */
const isMain = process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href;
if (isMain) publishNpm();
