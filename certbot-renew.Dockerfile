FROM docker:cli

RUN apk add --no-cache certbot

ENTRYPOINT ["/bin/sh", "-c"]
CMD ["trap exit TERM; while :; do certbot renew --deploy-hook 'docker exec accelerator-nginx nginx -s reload'; sleep 12h; done"]