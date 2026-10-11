FROM weknora/weknora-app:local-acceptance

WORKDIR /opt/openmaic-mcp
ENV NODE_ENV=production
COPY --chown=appuser:appuser tools/openmaic-mcp/package*.json ./
COPY --chown=appuser:appuser tools/openmaic-mcp/node_modules ./node_modules
COPY --chown=appuser:appuser tools/openmaic-mcp/dist ./dist
COPY --chown=appuser:appuser tools/openmaic-mcp/scripts ./scripts
RUN mkdir -p /app/data && chown appuser:appuser /app/data
USER appuser
ENTRYPOINT []
EXPOSE 3000
CMD ["node", "dist/index.js"]
