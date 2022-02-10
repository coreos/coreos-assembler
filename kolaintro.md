 # Kola Introduction Documentation
 
 This document just goes through the basics of how kola test's work and how someone just beginning can get started
 
 
 ### Useful Link
 
You can get more information about it, in the oficial documentation:
https://github.com/coreos/coreos-assembler/blob/main/docs/kola/external-tests.md

 
 #### Steps to run kola


Here we can see how to run kola: https://gitlab.cee.redhat.com/coreos/team-operations/-/blob/b96d3032611896d905f33907e5d2258706a1b5e1/PIPELINES.md#debugging



* `cosa kola run`
* `cosa kola --parallel run`
* `cosa kola basic`

### Command explanation


`cosa kola run 'name_of_test'` This is how to run a single test, This is used to help debug specific tests in order to get a better understanding of the bug that's taking place. Once you run this command this test will be added to the tmp directory

`cosa kola basic` This will just run the basic tests

`cosa kola --parallel` This will by default run 3 tests in a row

In order to see the logs for these tests you must enter the tmp/kola_test/name_of_the_tests and there you will find the logs (journal and console files, ignition used and so on)

`cosa run` This launches the build you created ( in this way you can access the image for troubleshooting ). Also check the option -c ( console). 

`cosa run -i ignition_path` You can run it passing your igniton, or the igntion used in the the test that failed for troubleshooting reasons.




### For additional tips I would recommend viewing this site:

https://github.com/coreos/coreos-assembler/blob/main/docs/working.md




 
 
 

 


