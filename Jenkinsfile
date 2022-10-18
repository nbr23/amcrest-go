pipeline {
    agent any
    stages {


        stage('Checkout'){
            steps {
                checkout scm
            }
        }
        stage('Dockerhub login') {
            steps {
                withCredentials([usernamePassword(credentialsId: 'dockerhub', usernameVariable: 'DOCKERHUB_CREDENTIALS_USR', passwordVariable: 'DOCKERHUB_CREDENTIALS_PSW')]) {
                    sh 'docker login -u $DOCKERHUB_CREDENTIALS_USR -p "$DOCKERHUB_CREDENTIALS_PSW"'
                }
            }
        }
        stage('Build Arch Images') {
            steps {
                sh '''
                    BUILDER=`docker buildx create --use`
                    docker buildx build --platform linux/amd64 --build-arg TARGET_ARCH=amd64 -t nbr23/amcrest-go:latest-amd64 . --push
                    docker buildx build --platform linux/arm64 --build-arg TARGET_ARCH=arm64 -t nbr23/amcrest-go:latest-arm64 . --push
                    docker buildx rm $BUILDER
                    '''
            }
        }
        stage('Push manifest') {
            steps {
                withCredentials([usernamePassword(credentialsId: 'dockerhub', usernameVariable: 'DOCKERHUB_CREDENTIALS_USR', passwordVariable: 'DOCKERHUB_CREDENTIALS_PSW')]) {
                    sh 'docker login -u $DOCKERHUB_CREDENTIALS_USR -p "$DOCKERHUB_CREDENTIALS_PSW"'
                }
                sh '''
                    docker manifest rm nbr23/amcrest-go:latest || true
                    docker manifest create nbr23/amcrest-go:latest -a nbr23/amcrest-go:latest-amd64 -a nbr23/amcrest-go:latest-arm64
                    docker manifest inspect nbr23/amcrest-go:latest
                    docker manifest push nbr23/amcrest-go:latest
                    '''
            }
        }
    }

    post {
        always {
            sh 'docker logout'
        }
    }
}