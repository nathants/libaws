FROM public.ecr.aws/lambda/nodejs:22.2026.08.20.17@sha256:27f7508ebf5adda871e16beda3527604be928292f86ed47402baccbfe49494c4
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
RUN corepack enable && pnpm install --frozen-lockfile --prod
COPY main.js ./
CMD ["main.handler"]
