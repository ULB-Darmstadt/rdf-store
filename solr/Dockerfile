FROM solr:latest

ENV JTS_VERSION=1.20.0
USER root
RUN su root && curl -O --output-dir ./server/solr-webapp/webapp/WEB-INF/lib https://repo1.maven.org/maven2/org/locationtech/jts/jts-core/${JTS_VERSION}/jts-core-${JTS_VERSION}.jar
USER $SOLR_UID