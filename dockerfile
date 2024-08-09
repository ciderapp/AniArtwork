# Use the latest Node.js image with Yarn pre-installed as a base
FROM node:22-alpine

# Set the working directory
WORKDIR /usr/src/app

# Install FFmpeg
RUN apk add --update-cache ffmpeg make gcc g++ python3

# Copy package.json and yarn.lock
COPY package.json yarn.lock ./

# Enable Corepack
RUN corepack enable

# Install dependencies using Yarn
RUN yarn install

# Copy the rest of the application code
COPY . .

# Expose the port the app runs on
EXPOSE 3000

# Define the command to run the application
CMD ["node", "server.mjs"]
