#[macro_use]
extern crate serde_derive;
extern crate serde;
extern crate serde_json;
extern crate serde_yaml;
extern crate tempfile;

use std::borrow::Cow;
use std::ops::Deref;
use std::path::Path;
use std::{env, fs, io, mem, process};

mod treefile;
use treefile::TreeComposeConfig;

// For convenience we allow the list to have multiple packages
// per item (intended for YAML).
fn whitespace_split_packages(pkgs: &Vec<String>) -> Vec<String> {
    let mut ret = Vec::with_capacity(pkgs.len());
    for pkg in pkgs {
        for pkg_item in pkg.split_whitespace() {
            ret.push(pkg_item.into());
        }
    }
    return ret;
}

fn manifest_data_to_tmpdir(
    path: &Path,
    manifest: &TreeComposeConfig,
) -> io::Result<tempfile::TempDir> {
    let tmpdir = tempfile::tempdir_in("/tmp")?;
    let postprocess_script: &str = manifest
        .postprocess_script
        .as_ref()
        .map_or("", String::as_str);
    // Handle unprefixed path
    let path = if path.as_os_str().is_empty() {
        Path::new(".")
    } else {
        path
    };
    for entry in fs::read_dir(path)? {
        let entry = entry?;
        let path = entry.path();
        if !path.is_file() {
            continue;
        }
        // Hardcoded list of external files
        let bn = entry.file_name();
        let bn = bn.to_str().unwrap();
        if bn.ends_with(".repo") || bn.ends_with(".json") || bn == "passwd" || bn == "group"
            || bn == postprocess_script
        {
            fs::copy(path, tmpdir.path().join(bn))?;
        }
    }
    return Ok(tmpdir);
}

fn is_yaml(name: &str) -> bool {
    name.ends_with(".yaml")
}

fn run() -> Result<(), String> {
    let mut manifest_index: Option<usize> = None;
    let env_args: Vec<String> = env::args().skip(1).collect();
    // Replace our argv with rpm-ostree compose tree [original argv]
    let base_args = &["compose", "tree"];
    let mut args: Vec<Cow<str>> = base_args
        .iter()
        .map(|v: &&str| *v)
        .chain(env_args.iter().map(|v| v.as_str()))
        .map(From::from)
        .collect();
    for (i, arg) in args.iter().enumerate() {
        if is_yaml(&arg) || arg.ends_with(".json") {
            manifest_index = Some(i);
        }
    }
    if manifest_index.is_none() {
        return Err("no manifest in arguments, expected *.yaml or *.json".into());
    }
    let manifest_index = manifest_index.unwrap();
    let manifest_path = (args[manifest_index]).to_string();
    let manifest_path = Path::new(&manifest_path);
    let manifest_f = fs::File::open(manifest_path).map_err(|err| err.to_string())?;

    // In the YAML case, we generate JSON from it in a temporary directory,
    // copying in the other files that are referenced by the manifest.
    let mut tmpd: Option<tempfile::TempDir> = None;
    if is_yaml(manifest_path.to_str().unwrap()) {
        let mut manifest: TreeComposeConfig =
            serde_yaml::from_reader(manifest_f).map_err(|err| err.to_string())?;
        if manifest.include.is_some() {
            return Err("include: is currently not supported in YAML syntax".into());
        }
        let new_pkgs = whitespace_split_packages(&manifest.packages);
        manifest.packages = new_pkgs;
        println!("Parsed manifest:");
        println!("  {:?}", manifest);

        tmpd = Some(
            manifest_data_to_tmpdir(manifest_path.parent().unwrap(), &manifest)
                .map_err(|err| err.to_string())?,
        );
        let tmpd_v = tmpd.as_ref().unwrap();
        let tmpd_path = tmpd_v.path();
        println!("Converting to JSON, tmpdir={:?}", tmpd_path);
        let bfn = manifest_path.file_name().unwrap();
        let bn = bfn.to_str().unwrap().replace(".yaml", ".json");
        let manifest_json_path = tmpd_path.join(bn);
        let out_json = fs::File::create(&manifest_json_path).map_err(|err| err.to_string())?;
        serde_json::to_writer_pretty(out_json, &manifest).map_err(|err| err.to_string())?;

        // Replace the YAML argument with JSON
        let manifest_path_str = manifest_json_path.to_str().unwrap();
        args[manifest_index] = Cow::Owned(manifest_path_str.to_string());
    }
    // libc::execve() is unsafe sadly, and also we want to clean up the tmpdir.
    // But we basically pass through all arguments other than the manifest
    // unchanged.
    println!("Executing: rpm-ostree {:?}", args);
    let status = process::Command::new("rpm-ostree")
        .args(args.iter().map(|v| v.deref()))
        .stdin(process::Stdio::null())
        .status()
        .map_err(|err| err.to_string())?;
    mem::forget(tmpd);
    if status.success() {
        Ok(())
    } else {
        Err(format!("rpm-ostree compose tree failed: {}", status))
    }
}

fn main() {
    ::process::exit(match run() {
        Ok(_) => 0,
        Err(e) => {
            println!("{}", e);
            1
        }
    })
}
