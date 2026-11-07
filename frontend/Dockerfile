# Construye la aplicación y la sirve con nginx, que deriva /api al gateway.
FROM node:22-alpine AS construccion
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY . .
ARG VITE_DATOS_FALSOS=true
ENV VITE_DATOS_FALSOS=$VITE_DATOS_FALSOS
RUN npm run build

FROM nginx:1.27-alpine
COPY nginx/default.conf.template /etc/nginx/templates/default.conf.template
COPY --from=construccion /app/dist /usr/share/nginx/html
ENV GATEWAY_URL=http://gateway:8080
EXPOSE 80
