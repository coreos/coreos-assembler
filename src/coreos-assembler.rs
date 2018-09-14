extern crate clap;
#[macro_use]
extern crate failure;

use failure::Error;
use clap::{App, SubCommand};

fn hello() -> Result<(), Error> {
    println!("🦀");
    Ok(())
}

fn run() -> Result<(), Error> {
    let matches = App::new("coreos-assembler")
        .version("0.1")
        .about("CoreOS assembler")
        .subcommand(
            SubCommand::with_name("hello")
                .about("Say hello")
        )
        .get_matches();

    match matches.subcommand() {
        ("hello", _) => hello(),
        ("", _) => bail!("No command given"),
        _ => unreachable!(),
    }
}

fn main() {
    match run() {
        Ok(_) => {}
        Err(e) => {
            eprintln!("{:?}", e);
            std::process::exit(1)
        }
    }
}
