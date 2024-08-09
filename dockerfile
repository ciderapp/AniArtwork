# Use the latest Node.js image with Yarn pre-installed as a base
FROM oven/bun:canary-alpine

# Set the working directory
WORKDIR /usr/src/app

# Install FFmpeg
RUN apk add --update-cache ffmpeg make gcc g++ python3

# Copy package.json and yarn.lock
COPY package.json yarn.lock ./

# Install dependencies using Yarn
RUN bun install

# Copy the rest of the application code
COPY . .

# Expose the port the app runs on
EXPOSE 3000

# Define the command to run the application
CMD ["bun", "start"]
