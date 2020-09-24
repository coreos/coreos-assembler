// Copyright 2016 CoreOS, Inc.
// Copyright 2008 The Android Open Source Project
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// repo is a limited implementation of the python repo git front end.
//
// Manifest Format
//
// A repo manifest describes the structure of a repo client; that is
// the directories that are visible and where they should be obtained
// from with git.
//
// The basic structure of a manifest is a bare Git repository holding
// a single 'default.xml' XML file in the top level directory.
//
// Manifests are inherently version controlled, since they are kept
// within a Git repository.  Updates to manifests are automatically
// obtained by clients during `repo sync`.
//
// A manifest XML file (e.g. 'default.xml') roughly conforms to the
// following DTD. The python code is the only authoritative source.
//
// Local Manifests
//
// Additional remotes and projects may be added through local manifest
// files stored in `$TOP_DIR/.repo/local_manifests/*.xml`.
//
// For example:
//
//   $ ls .repo/local_manifests
//   local_manifest.xml
//   another_local_manifest.xml
//
//   $ cat .repo/local_manifests/local_manifest.xml
//   <?xml version="1.0" encoding="UTF-8"?>
//   <manifest>
//     <project path="manifest"
//              name="tools/manifest" />
//     <project path="platform-manifest"
//              name="platform/manifest" />
//   </manifest>
//
// Users may add projects to the local manifest(s) prior to a `repo sync`
// invocation, instructing repo to automatically download and manage
// these extra projects.
//
// Manifest files stored in `$TOP_DIR/.repo/local_manifests/*.xml` will
// be loaded in alphabetical order.
//
// Additional remotes and projects may also be added through a local
// manifest, stored in `$TOP_DIR/.repo/local_manifest.xml`. This method
// is deprecated in favor of using multiple manifest files as mentioned
// above.
//
// If `$TOP_DIR/.repo/local_manifest.xml` exists, it will be loaded before
// any manifest files stored in `$TOP_DIR/.repo/local_manifests/*.xml`.
package repo

import (
	"encoding/xml"
)

// Manifest is the root element of the file.
//
//    <!ELEMENT manifest (include*,
//                        notice?,
//                        remote*,
//                        default?,
//                        manifest-server?,
//                        project*,
//                        extend-project*,
//                        remove-project*,
//                        repo-hooks?)>
//
//    <!ELEMENT notice (#PCDATA)>
//
type Manifest struct {
	XMLName        xml.Name        `xml:"manifest"`
	Includes       []Include       `xml:"include"`
	Notice         string          `xml:"notice"`
	Remotes        []Remote        `xml:"remote"`
	Default        *Default        `xml:"default"`
	ManifestServer *ManifestServer `xml:"manifest-server"`
	Projects       []Project       `xml:"project"`
	ExtendProjects []ExtendProject `xml:"extend-project"`
	RemoveProjects []RemoveProject `xml:"remove-project"`
	RepoHooks      *RepoHooks      `xml:"repo-hooks"`
}

// Remote
//
//    <!ELEMENT remote (EMPTY)>
//    <!ATTLIST remote name         ID    #REQUIRED>
//    <!ATTLIST remote alias        CDATA #IMPLIED>
//    <!ATTLIST remote fetch        CDATA #REQUIRED>
//    <!ATTLIST remote review       CDATA #IMPLIED>
//    <!ATTLIST remote revision     CDATA #IMPLIED>
//
// One or more remote elements may be specified.  Each remote element
// specifies a Git URL shared by one or more projects and (optionally)
// the Gerrit review server those projects upload changes through.
//
// Attribute `name`: A short name unique to this manifest file.  The
// name specified here is used as the remote name in each project's
// .git/config, and is therefore automatically available to commands
// like `git fetch`, `git remote`, `git pull` and `git push`.
//
// Attribute `alias`: The alias, if specified, is used to override
// `name` to be set as the remote name in each project's .git/config.
// Its value can be duplicated while attribute `name` has to be unique
// in the manifest file. This helps each project to be able to have
// same remote name which actually points to different remote url.
//
// Attribute `fetch`: The Git URL prefix for all projects which use
// this remote.  Each project's name is appended to this prefix to
// form the actual URL used to clone the project.
//
// Attribute `review`: Hostname of the Gerrit server where reviews
// are uploaded to by `repo upload`.  This attribute is optional;
// if not specified then `repo upload` will not function.
//
// Attribute `revision`: Name of a Git branch (e.g. `master` or
// `refs/heads/master`). Remotes with their own revision will override
// the default revision.
//
type Remote struct {
	Name     string `xml:"name,attr"`
	Alias    string `xml:"alias,attr,omitempty"`
	Fetch    string `xml:"fetch,attr"`
	Review   string `xml:"review,attr,omitempty"`
	Revision string `xml:"revision,attr,omitempty"`
}

// Default
//
//    <!ELEMENT default (EMPTY)>
//    <!ATTLIST default remote      IDREF #IMPLIED>
//    <!ATTLIST default revision    CDATA #IMPLIED>
//    <!ATTLIST default dest-branch CDATA #IMPLIED>
//    <!ATTLIST default sync-j      CDATA #IMPLIED>
//    <!ATTLIST default sync-c      CDATA #IMPLIED>
//    <!ATTLIST default sync-s      CDATA #IMPLIED>
//
// At most one default element may be specified.  Its remote and
// revision attributes are used when a project element does not
// specify its own remote or revision attribute.
//
// Attribute `remote`: Name of a previously defined remote element.
// Project elements lacking a remote attribute of their own will use
// this remote.
//
// Attribute `revision`: Name of a Git branch (e.g. `master` or
// `refs/heads/master`).  Project elements lacking their own
// revision attribute will use this revision.
//
// Attribute `dest-branch`: Name of a Git branch (e.g. `master`).
// Project elements not setting their own `dest-branch` will inherit
// this value. If this value is not set, projects will use `revision`
// by default instead.
//
// Attribute `sync-j`: Number of parallel jobs to use when synching.
//
// Attribute `sync-c`: Set to true to only sync the given Git
// branch (specified in the `revision` attribute) rather than the
// whole ref space.  Project elements lacking a sync-c element of
// their own will use this value.
//
// Attribute `sync-s`: Set to true to also sync sub-projects.
//
type Default struct {
	Remote          string `xml:"remote,attr,omitempty"`
	Revision        string `xml:"revision,attr,omitempty"`
	DestBranch      string `xml:"dest-branch,attr,omitempty"`
	SyncJobs        string `xml:"sync-j,attr,omitempty"`
	SyncBranch      string `xml:"sync-c,attr,omitempty"`
	SyncSubProjects string `xml:"sync-s,attr,omitempty"`
}

// ManifestServer
//
//    <!ELEMENT manifest-server (EMPTY)>
//    <!ATTLIST url              CDATA #REQUIRED>
//
// At most one manifest-server may be specified. The url attribute
// is used to specify the URL of a manifest server, which is an
// XML RPC service.
//
// The manifest server should implement the following RPC methods:
//
//   GetApprovedManifest(branch, target)
//
// Return a manifest in which each project is pegged to a known good revision
// for the current branch and target.
//
// The target to use is defined by environment variables TARGET_PRODUCT
// and TARGET_BUILD_VARIANT. These variables are used to create a string
// of the form $TARGET_PRODUCT-$TARGET_BUILD_VARIANT, e.g. passion-userdebug.
// If one of those variables or both are not present, the program will call
// GetApprovedManifest without the target parameter and the manifest server
// should choose a reasonable default target.
//
//   GetManifest(tag)
//
// Return a manifest in which each project is pegged to the revision at
// the specified tag.
//
type ManifestServer struct {
	URL string `xml:"url,attr"`
}

// Project
//
//    <!ELEMENT project (annotation*,
//                       project*,
//                       copyfile*,
//                       linkfile*)>
//    <!ATTLIST project name        CDATA #REQUIRED>
//    <!ATTLIST project path        CDATA #IMPLIED>
//    <!ATTLIST project remote      IDREF #IMPLIED>
//    <!ATTLIST project revision    CDATA #IMPLIED>
//    <!ATTLIST project dest-branch CDATA #IMPLIED>
//    <!ATTLIST project groups      CDATA #IMPLIED>
//    <!ATTLIST project sync-c      CDATA #IMPLIED>
//    <!ATTLIST project sync-s      CDATA #IMPLIED>
//    <!ATTLIST project upstream    CDATA #IMPLIED>
//    <!ATTLIST project clone-depth CDATA #IMPLIED>
//    <!ATTLIST project force-path  CDATA #IMPLIED>
//
// One or more project elements may be specified.  Each element
// describes a single Git repository to be cloned into the repo
// client workspace.  You may specify Git-submodules by creating a
// nested project.  Git-submodules will be automatically
// recognized and inherit their parent's attributes, but those
// may be overridden by an explicitly specified project element.
//
// Attribute `name`: A unique name for this project.  The project's
// name is appended onto its remote's fetch URL to generate the actual
// URL to configure the Git remote with.  The URL gets formed as:
//
//   ${remote_fetch}/${project_name}.git
//
// where ${remote_fetch} is the remote's fetch attribute and
// ${project_name} is the project's name attribute.  The suffix ".git"
// is always appended as repo assumes the upstream is a forest of
// bare Git repositories.  If the project has a parent element, its
// name will be prefixed by the parent's.
//
// The project name must match the name Gerrit knows, if Gerrit is
// being used for code reviews.
//
// Attribute `path`: An optional path relative to the top directory
// of the repo client where the Git working directory for this project
// should be placed.  If not supplied the project name is used.
// If the project has a parent element, its path will be prefixed
// by the parent's.
//
// Attribute `remote`: Name of a previously defined remote element.
// If not supplied the remote given by the default element is used.
//
// Attribute `revision`: Name of the Git branch the manifest wants
// to track for this project.  Names can be relative to refs/heads
// (e.g. just "master") or absolute (e.g. "refs/heads/master").
// Tags and/or explicit SHA-1s should work in theory, but have not
// been extensively tested.  If not supplied the revision given by
// the remote element is used if applicable, else the default
// element is used.
//
// Attribute `dest-branch`: Name of a Git branch (e.g. `master`).
// When using `repo upload`, changes will be submitted for code
// review on this branch. If unspecified both here and in the
// default element, `revision` is used instead.
//
// Attribute `groups`: List of groups to which this project belongs,
// whitespace or comma separated.  All projects belong to the group
// "all", and each project automatically belongs to a group of
// its name:`name` and path:`path`.  E.g. for
// <project name="monkeys" path="barrel-of"/>, that project
// definition is implicitly in the following manifest groups:
// default, name:monkeys, and path:barrel-of.  If you place a project in the
// group "notdefault", it will not be automatically downloaded by repo.
// If the project has a parent element, the `name` and `path` here
// are the prefixed ones.
//
// Attribute `sync-c`: Set to true to only sync the given Git
// branch (specified in the `revision` attribute) rather than the
// whole ref space.
//
// Attribute `sync-s`: Set to true to also sync sub-projects.
//
// Attribute `upstream`: Name of the Git ref in which a sha1
// can be found.  Used when syncing a revision locked manifest in
// -c mode to avoid having to sync the entire ref space.
//
// Attribute `clone-depth`: Set the depth to use when fetching this
// project.  If specified, this value will override any value given
// to repo init with the --depth option on the command line.
//
// Attribute `force-path`: Set to true to force this project to create the
// local mirror repository according to its `path` attribute (if supplied)
// rather than the `name` attribute.  This attribute only applies to the
// local mirrors syncing, it will be ignored when syncing the projects in a
// client working directory.
//
type Project struct {
	Annotations     []Annotation `xml:"annotation"`
	SubProjects     []Project    `xml:"project"`
	CopyFiles       []CopyFile   `xml:"copyfile"`
	LinkFiles       []LinkFile   `xml:"linkfile"`
	Name            string       `xml:"name,attr"`
	Path            string       `xml:"path,attr,omitempty"`
	Remote          string       `xml:"remote,attr,omitempty"`
	Revision        string       `xml:"revision,attr,omitempty"`
	DestBranch      string       `xml:"dest-branch,attr,omitempty"`
	Groups          string       `xml:"groups,attr,omitempty"`
	SyncBranch      string       `xml:"sync-c,attr,omitempty"`
	SyncSubProjects string       `xml:"sync-s,attr,omitempty"`
	Upstream        string       `xml:"upstream,attr,omitempty"`
	CloneDepth      string       `xml:"clone-depth,attr,omitempty"`
	ForcePath       string       `xml:"force-path,attr,omitempty"`
}

// ExtendProject
//
//    <!ELEMENT extend-project>
//    <!ATTLIST extend-project name   CDATA #REQUIRED>
//    <!ATTLIST extend-project path   CDATA #IMPLIED>
//    <!ATTLIST extend-project groups CDATA #IMPLIED>
//
// Modify the attributes of the named project.
//
// This element is mostly useful in a local manifest file, to modify the
// attributes of an existing project without completely replacing the
// existing project definition.  This makes the local manifest more robust
// against changes to the original manifest.
//
// Attribute `path`: If specified, limit the change to projects checked out
// at the specified path, rather than all projects with the given name.
//
// Attribute `groups`: List of additional groups to which this project
// belongs.  Same syntax as the corresponding element of `project`.
//
type ExtendProject struct {
	Name   string `xml:"name,attr"`
	Path   string `xml:"path,attr,omitempty"`
	Groups string `xml:"groups,attr,omitempty"`
}

// Annotation
//
//    <!ELEMENT annotation (EMPTY)>
//    <!ATTLIST annotation name  CDATA #REQUIRED>
//    <!ATTLIST annotation value CDATA #REQUIRED>
//    <!ATTLIST annotation keep  CDATA "true">
//
// Zero or more annotation elements may be specified as children of a
// project element. Each element describes a name-value pair that will be
// exported into each project's environment during a 'forall' command,
// prefixed with REPO__.  In addition, there is an optional attribute
// "keep" which accepts the case insensitive values "true" (default) or
// "false".  This attribute determines whether or not the annotation will
// be kept when exported with the manifest subcommand.
//
type Annotation struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
	Keep  string `xml:"keep,attr,omitempty"`
}

// CopyFile
//
//    <!ELEMENT copyfile (EMPTY)>
//    <!ATTLIST src value  CDATA #REQUIRED>
//    <!ATTLIST dest value CDATA #REQUIRED>
//
// Zero or more copyfile elements may be specified as children of a
// project element. Each element describes a src-dest pair of files;
// the "src" file will be copied to the "dest" place during 'repo sync'
// command.
// "src" is project relative, "dest" is relative to the top of the tree.
//
type CopyFile struct {
	Src  string `xml:"src,attr"`
	Dest string `xml:"dest,attr"`
}

// LinkFile
//
//    <!ELEMENT linkfile (EMPTY)>
//    <!ATTLIST src value  CDATA #REQUIRED>
//    <!ATTLIST dest value CDATA #REQUIRED>
//
// It's just like copyfile and runs at the same time as copyfile but
// instead of copying it creates a symlink.
//
type LinkFile struct {
	Src  string `xml:"src,attr"`
	Dest string `xml:"dest,attr"`
}

// RemoveProject
//
//    <!ELEMENT remove-project (EMPTY)>
//    <!ATTLIST remove-project name CDATA #REQUIRED>
//
// Deletes the named project from the internal manifest table, possibly
// allowing a subsequent project element in the same manifest file to
// replace the project with a different source.
//
// This element is mostly useful in a local manifest file, where
// the user can remove a project, and possibly replace it with their
// own definition.
//
type RemoveProject struct {
	Name string `xml:"name,attr"`
}

// RepoHooks
//
//    <!ELEMENT repo-hooks (EMPTY)>
//    <!ATTLIST repo-hooks in-project   CDATA #REQUIRED>
//    <!ATTLIST repo-hooks enabled-list CDATA #REQUIRED>
//
type RepoHooks struct {
	InProject   string `xml:"in-project,attr"`
	EnabledList string `xml:"enabled-list,attr"`
}

// Include
//
//    <!ELEMENT include      (EMPTY)>
//    <!ATTLIST include name CDATA #REQUIRED>
//
// This element provides the capability of including another manifest
// file into the originating manifest.  Normal rules apply for the
// target manifest to include - it must be a usable manifest on its own.
//
// Attribute `name`: the manifest to include, specified relative to
// the manifest repository's root.
//
type Include struct {
	Name string `xml:"name,attr"`
}
