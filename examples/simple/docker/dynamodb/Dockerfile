FROM public.ecr.aws/lambda/nodejs:24.2026.09.13.11@sha256:9447dbf363ef0959a47e36f9f32694c7ae77e7bd1c6b7aaac6ba406bc6ea54c7
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
RUN corepack enable && pnpm install --frozen-lockfile --prod
COPY main.js ./
CMD ["main.handler"]
