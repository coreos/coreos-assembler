 # Kola Introduction Documentation
 
This document goes through the basics of how kola tests work and how someone just beginning can get started
 
### What is Kola?
Kola is a framework for testing software integration in CoreOS systems across multiple platforms. It is primarily designed to operate within the CoreOS Assembler for testing software that has landed in the OS image.


> [Additional documentation about Kola](https://github.com/coreos/coreos-assembler/tree/main/docs/kola)
 
 ### Steps to run a test from start to finish
 
 1. `cosa run` (Creates and brings you into the shell of a QEMU instance, running the provided version of FCOS or RHCOS)
 2. `cosa kola list` (Here we can see all of the tests that we can do)
 3. `cosa kola run <test-pattern>` (This will run a specific test and check to see if a test passes)
 4. `cosa kola run ext.config*` (This will run all the tests that start with `ext.config`)

### More information on tests

There are file logs in each of these locations: They will help you to debug the problem and will certainly give you hints along the way

1. `journal.txt`
2. `console.txt` 
3. `ignition.json` 
4. `gunzip journal-raw.txt.gz` 



### Extended artifacts

1. Extended artefacts need additional forms of testing (You can pass the ignition and the path to the artefact you want to test)
2. `cosa kola run -h` (this allows you to see the commands yourself and what syntax is needed)
2. `cosa buildextend-"name_of_artifact` (An example of building an artefact before testing it) 
3. `kola run -p <platform>` Is the most generic way of testing extended artifacts, this is mostly useful for the cloud platforms
4. For running the likes of metal/metal4k artifacts there's not much difference than running `kola run` from the coreos-assembler
5. `cd builds/latest/` (This will show your latest build information)
6. In the case of the `testiso` command, you'll see that there is the `--qemu-native-4k` option passed to `kola testiso`.  This instructs the `testiso` test to attempt to install FCOS/RHCOS to a disk that uses 4k sector size.  If you don't include that option, the `testiso` command will attempt to install FCOS/RHCOS to a non 4k disk (512b sector size)
7. `kola testiso -S --scenarios pxe-install,pxe-offline-install --output-dir tmp/kola-metal` 
8. `cosa kola testiso --qemu-native-4k` (This is an example of me testing the live ISO build for a 4k sectors disk, this tests all of the scenarios)

Example output:

```
kola -p qemu-unpriv --output-dir tmp/kola testiso -P --qemu-native-4k
Testing scenarios: [iso-offline-install iso-live-login iso-as-disk miniso-install miniso-install-nm]
Detected development build; disabling signature verification
Successfully tested scenario iso-offline-install for 35.20220217.dev.0 on uefi (metal4k)
Successfully tested scenario iso-live-login for 35.20220217.dev.0 on uefi (metal4k)
Successfully tested scenario iso-as-disk for 35.20220217.dev.0 on uefi (metal4k)
Successfully tested scenario miniso-install for 35.20220217.dev.0 on uefi (metal4k)
Successfully tested scenario miniso-install-nm for 35.20220217.dev.0 on uefi (metal4k with NM keyfile)
```





> [This is a good starting point:](https://gitlab.cee.redhat.com/coreos/redhat-coreos/-/blob/master/upshift/osbuild.groovy#L318-371)




### Command explanation

`cosa kola run 'name_of_test'` This is how to run a single test, This is used to help debug specific tests in order to get a better understanding of the bug that's taking place. Once you run this command this test will be added to the tmp directory

`cosa kola basic` This will just run the basic tests

`cosa kola --parallel` This will by default run 3 tests in a row

In order to see the logs for these tests you must enter the `tmp/kola_test/name_of_the_tests` and there you will find the logs (journal and console files, ignition used and so on)

`cosa run` This launches the build you created ( in this way you can access the image for troubleshooting ). Also check the option -c ( console). 

`cosa run -i ignition_path` You can run it passing your igniton, or the igntion used in the the test that failed for troubleshooting reasons.





