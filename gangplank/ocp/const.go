package ocp

const (
	// ocpStructTag is the struct tag used to read in
	// OCPBuilder from envvars
	ocpStructTag = "envVar"

	// defaultContextdir is the default path to use for a build
	defaultContextDir = "/srv"

	// secretLabelName is the label to search for secrets to automatically use
	secretLabelName = "coreos-assembler.coreos.com/secret"

	// cosaSrvCache is the location of the cache files
	cosaSrvCache = "/srv/cache"

	// cosaSrvTmpRepo is the location the repo files
	cosaSrvTmpRepo = "/srv/tmp/repo"

	// cacheTarballName is the name of the file used when Stage.{Require,Return}Cache is true
	cacheTarballName = "cache.tar.gz"

	// cacheRepoTarballName is the name of the file used when Stage.{Require,Return}RepoCache is true
	cacheRepoTarballName = "repo.tar.gz"

	// cacheBucket is used for storing the cache
	cacheBucket = "cache"
)
