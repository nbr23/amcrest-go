@Library('jenkins-shared-library') _

pipeline {
    agent any
    stages {
        stage('Checkout'){
            steps {
                checkout scm
            }
        }
        stage('Prep buildx') {
            steps {
                script {
                    env.BUILDX_BUILDER = getBuildxBuilder();
                    sh 'docker manifest rm nbr23/amcrest-go:latest || true'
                }
            }
        }
        stage('Build and push multiarch image') {
            steps {
                withCredentials([usernamePassword(credentialsId: 'dockerhub', usernameVariable: 'DOCKERHUB_CREDENTIALS_USR', passwordVariable: 'DOCKERHUB_CREDENTIALS_PSW')]) {
                    sh 'docker login -u $DOCKERHUB_CREDENTIALS_USR -p "$DOCKERHUB_CREDENTIALS_PSW"'
                }
                sh """
                    docker buildx build --builder \$BUILDX_BUILDER --platform linux/arm64,linux/amd64 -t nbr23/amcrest-go:latest . ${GIT_BRANCH == "master" ? "--push":""}
                    """
            }
        }
        stage('Sync github repos') {
            when { branch 'master' }
            steps {
                syncRemoteBranch('git@github.com:nbr23/amcrest-go.git', 'master')
            }
        }
    }

    post {
        always {
            sh 'docker buildx stop $BUILDX_BUILDER || true'
            sh 'docker buildx rm $BUILDX_BUILDER'
        }
    }
}