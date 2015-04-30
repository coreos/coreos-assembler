# Mantle Roadmap

Mantle is somewhat of a catch-all project for Go-based development tools
in the CoreOS SDK. Plans are divided up between the main components.

## plume

The eventual goal of this command is to automate the process of
uploading and publishing images, replacing bash scripts like
core_promote and oem/ami/*

## kola

 - Refactor existing code into a general test framework and harness to
   make plugging in small new tests quick and relatively simple.
 - Replace/port any useful tests that currently live in coretest.
 - Add complete testing of the update process, exercising update_engine
   and the gptprio GRUB module. To test the update process an embedded
   Omaha server that serves an update payload needs to be embedded into
   Kola to replace the python code in devserver that exists currently.
 - Add more extensive docker functional tests. Check the docker project
   to see if there are existing tests we can port.
 - Support execution of the existing etcd, rkt, and fleet functional
   tests that live in their respective code bases.
