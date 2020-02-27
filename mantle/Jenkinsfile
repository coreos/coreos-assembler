#!groovy

properties([
    buildDiscarder(logRotator(daysToKeepStr: '20', numToKeepStr: '30')),

    [$class: 'CopyArtifactPermissionProperty',
     projectNames: '*'],

    parameters([
        choice(name: 'GOARCH',
               choices: "amd64\narm64\ns390x",
               description: 'target architecture for building binaries')
    ]),

    pipelineTriggers([pollSCM('H/15 * * * *')])
])

node('amd64 && docker') {
    stage('SCM') {
        checkout scm
    }

    stage('Build') {
        sh "docker run --rm -e CGO_ENABLED=1 -e GOARCH=${params.GOARCH} -e GOCACHE=/usr/src/myapp/cache -u \"\$(id -u):\$(id -g)\" -v /etc/passwd:/etc/passwd:ro -v /etc/group:/etc/group:ro -v \"\$PWD\":/usr/src/myapp -w /usr/src/myapp golang:1.12 ./build"
    }

    stage('Test') {
        sh 'docker run --rm -e GOCACHE=/usr/src/myapp/cache -u "$(id -u):$(id -g)" -v /etc/passwd:/etc/passwd:ro -v /etc/group:/etc/group:ro -v "$PWD":/usr/src/myapp -w /usr/src/myapp golang:1.12 ./test'
    }

    stage('Post-build') {
        if (env.JOB_BASE_NAME == "master-builder") {
            archiveArtifacts artifacts: 'bin/**', fingerprint: true, onlyIfSuccessful: true
        }
    }
}
