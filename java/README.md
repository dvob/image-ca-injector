# Java Tests

```
docker build --build-arg=openjdk -t java-http-get java/

docker run -it --rm --network image-ca-injector-test java-http-get

image-ca-injector -dst docker openjdk myopenjdk test-certs/myca.crt

docker build --build-arg=myopenjdk -t java-http-get java/

docker run -it --rm --network image-ca-injector-test java-http-get
```
