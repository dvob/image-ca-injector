ARG JAVA_IMAGE=image-ca-injector-java-test

FROM $JAVA_IMAGE

COPY HttpGet.java .

RUN javac HttpGet.java

ENTRYPOINT java -cp . HttpGet https://test.example.com
