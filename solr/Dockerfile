FROM solr:10

ENV JTS_VERSION=1.20.0
USER root
RUN su root && curl -O --output-dir ./server/solr-webapp/webapp/WEB-INF/lib https://repo1.maven.org/maven2/org/locationtech/jts/jts-core/${JTS_VERSION}/jts-core-${JTS_VERSION}.jar
# The rdf configset matches the collection name so solr create uses it. It
# raises the schemaless field limit from the default 1000 to 10000, which the
# hashed query fields (_query_.*) of the dataset require.
COPY configsets /opt/solr/server/solr/configsets
USER $SOLR_UID