 # Kola Introduction Documentation
 
 This document just goes through the basics of how kola test's work and how someone just beginning can get started
 


 
 #### Steps to run kola


1. mkdir src
2. git clone https://github.com/openshift/os src/config
3. pushd src/config
4. git fetch --all
5. git checkout origin/master
6. git submodule update --init --recursive
7. cd /src
8. git clone --branch master https://gitlab.cee.redhat.com/coreos/redhat-coreos.git src/tmp
9. cp -Rf src/tmp/*.repo src/config/
10. coreos-assembler init --force /srv/config
11. cosa fetch
12. cosa build
13. cosa buildextend-metal
    



* `cosa kola run`
* `cosa kola --parallel run`
* `cosa kola basic`

### Versions older than 4.9

1. cd /home/builder 
2. mkdir src
3. git clone https://gitlab.cee.redhat.com/coreos/redhat-coreos.git src/config
4. pushd src/config
5. git fetch --all
6. git checkout origin/master
7. git submodule update --init --recursive
8. cd /home/builder 
9. coreos-assembler init --force /home/jenkins/agent/workspace/rhcos/rhcos-rhcos-4.8/src/config
10. cosa fetch
11. cosa build
12. cosa buildextend-metal
13. cosa buildextend-metal4k
14. cosa buildextend-live
15. cosa buildextend-installer
16. cosa kola run --basic-qemu-scenarios
17. cosa kola run -d --parallel 3



### Command explanation


`cosa kola run 'name_of_test'` This is how to run a single test, This is used to help debug specific tests in order to get a better understanding of the bug that's taking place. Once you run this command this test will be added to the tmp directory

`cosa kola basic` This will just run the basic tests

`cosa kola --parallel` This will by default run 3 tests in a row

In order to see the logs for these tests you must enter the tmp/kola_test/name_of_the_tests and there you will find the logs (journal and console files, ignition used and so on)

`cosa run` This launches the build you created ( in this way you can access the image for troubleshooting ). Also check the option -c ( console). 

`cosa run -i ignition_path` You can run it passing your igniton, or the igntion used in the the test that failed for troubleshooting reasons.







 
 
 

 


