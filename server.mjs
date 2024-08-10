import express from 'express';
import { createRequire } from 'module';
const require = createRequire(import.meta.url);
import crypto from 'crypto';
import Queue from 'bull';
import path from 'path';
import fsSync from 'fs/promises'; // Use the promises API for async operations
import fs from 'fs';
import { fileURLToPath } from 'url';
import winston from 'winston';
import { promisify } from 'util';
const ffmpeg = require('fluent-ffmpeg');
const { default: fetch } = await import('node-fetch');
const exec = promisify(require('child_process').exec);

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const app = express();
const port = 3000;
const cacheDir = path.join(__dirname, 'cache');

// Ensure cache directory exists
fsSync.mkdir(cacheDir, { recursive: true }).catch(err => logger.error(`Error creating cache directory: ${err.message}`));

// Configure logging
const logger = winston.createLogger({
    level: 'info',
    format: winston.format.combine(
        winston.format.colorize(),
        winston.format.timestamp(),
        winston.format.printf(({ timestamp, level, message }) => `${timestamp} ${level}: ${message}`)
    ),
    transports: [
        new winston.transports.Console(),
        new winston.transports.File({ filename: 'server.log', format: winston.format.uncolorize() })
    ]
});

// Function to validate GIF files
const validateGIF = async (gifPath) => {
    try {
        await exec(`ffmpeg -v error -i ${gifPath} -f null -`);
        return true;
    } catch (error) {
        return false;
    }
};

// Function to clean invalid GIFs from cache
const cleanCache = async () => {
    logger.info('Starting cache cleanup...');
    try {
        const files = (await fsSync.readdir(cacheDir)).filter(file => file.endsWith('.gif'));

        for (const file of files) {
            const filePath = path.join(cacheDir, file);
            const isValid = await validateGIF(filePath);
            if (!isValid) {
                logger.warn(`Invalid GIF found and removed: ${file}`);
                await fsSync.unlink(filePath);
            } else {
                logger.info(`Valid GIF: ${file}`);
            }
        }
        logger.info('Cache cleanup completed.');
    } catch (err) {
        logger.error(`Cache cleanup error: ${err.message}`);
    }
};

// Perform cache cleanup on server startup
cleanCache().catch(err => {
    logger.error(`Cache cleanup error: ${err.message}`);
});

// Job queue for processing streams
const processQueue = new Queue('processQueue', {
    redis: {
        host: '10.10.79.15', // change this if your Redis server is hosted elsewhere
        port: 6379
    },
    limiter: {
        max: 5, // Maximum 5 concurrent jobs
        duration: 1000
    }
});

processQueue.on('error', (error) => {
    logger.error(`Queue error: ${error.message}`);
});

processQueue.on('failed', (job, err) => {
    logger.error(`Job ${job.id} failed: ${err.message}`);
});

// Function to generate a unique key based on the URL
const generateKey = (url) => crypto.createHash('md5').update(url).digest('hex');

// Function to process the m3u8 stream and save as GIF
const processStream = async (url, key, jobId) => {
    logger.info(`Job ${jobId}: Starting processing for URL ${url}`);
    return new Promise((resolve, reject) => {
        const gifPath = path.join(cacheDir, `${key}.gif`);

        ffmpeg(url)
            .inputOptions('-protocol_whitelist file,http,https,tcp,tls,crypto')
            .output(gifPath)
            .outputOptions('-vf', 'fps=15,scale=500:-1:flags=lanczos')
            .outputOptions('-threads', '8')
            .outputOptions('-preset', 'fast')
            .outputOptions('-multiple_requests', '1')
            .outputOptions('-buffer_size', '8192k')
            .on('start', (commandLine) => {
                logger.info(`Job ${jobId}: FFmpeg started with command: ${commandLine}`);
            })
            .on('progress', (progress) => {
                logger.info(`Job ${jobId}: Processing - ${JSON.stringify(progress)}`);
            })
            .on('end', () => {
                logger.info(`Job ${jobId}: Processing completed for URL ${url}`);
                resolve(gifPath);
            })
            .on('error', (err) => {
                logger.error(`Job ${jobId}: Error processing URL ${url} - ${err.message}`);
                reject(err);
            })
            .run();
    });
};

// Process job queue
processQueue.process(3, async (job) => {
    const { url, key, jobId } = job.data;
    const gifPath = path.join(cacheDir, `${key}.gif`);

    logger.info(`Job ${jobId}: Starting job for URL ${url}`);

    if (fs.existsSync(gifPath)) {
        logger.info(`Job ${jobId}: GIF already exists for URL ${url}.gif`);
        return gifPath;
    }

    try {
        const result = await processStream(url, key, jobId);
        logger.info(`Job ${jobId}: Job completed for URL ${url}`);
        return result;
    } catch (error) {
        logger.error(`Job ${jobId}: Error in job for URL ${url} - ${error.message}`);
        throw new Error('Error processing the stream');
    }
});

// Route to generate GIF
app.get('/artwork/generate', async (req, res) => {
    const url = req.query.url;
    if (!url) {
        return res.status(400).send('URL query parameter is required');
    }
    
    const parsedUrl = new URL(url);
    if (parsedUrl.hostname !== 'mvod.itunes.apple.com') {
        logger.warn(`Invalid domain: ${parsedUrl.hostname}`);
        return res.status(400).send('Only URLs from mvod.itunes.apple.com are allowed');
    }

    try {
        const response = await fetch(url);
        if (!response.ok) {
            logger.error(`Failed to fetch URL ${url}: ${response.statusText}`);
            return res.status(400).send('Failed to fetch the m3u8 URL');
        }
    } catch (error) {
        logger.error(`Error fetching URL ${url}: ${error.message}`);
        return res.status(400).send('Error fetching the m3u8 URL');
    }

    const key = generateKey(url);
    const gifPath = path.join(cacheDir, `${key}.gif`);
    const jobId = crypto.randomBytes(4).toString('hex');

    logger.info(`Job ${jobId}: Received to generate GIF for URL ${url}`);

    if (fs.existsSync(gifPath)) {
        // Set cache headers for Cloudflare
        const sevenDaysInSeconds = 7 * 24 * 60 * 60;
        const expiresDate = new Date(Date.now() + sevenDaysInSeconds * 1000).toUTCString();

        res.setHeader('Cache-Control', `public, max-age=${sevenDaysInSeconds}`);
        res.setHeader('Expires', expiresDate);

        logger.info(`Job ${jobId}: Served existing GIF on URL https://art.cider.sh/artwork/${key}.gif`);
        return res.status(200).json({ key, message: 'GIF already exists', url: `https://art.cider.sh/artwork/${key}.gif` });
    }

    processQueue.add({ url, key, jobId }).then((job) => {
        logger.info(`Job ${jobId}: Added to the queue for URL ${url}`);
        job.finished().then(() => {
            // Set cache headers for Cloudflare
            const sevenDaysInSeconds = 7 * 24 * 60 * 60;
            const expiresDate = new Date(Date.now() + sevenDaysInSeconds * 1000).toUTCString();

            res.setHeader('Cache-Control', `public, max-age=${sevenDaysInSeconds}`);
            res.setHeader('Expires', expiresDate);
            
            logger.info(`Job ${jobId}: GIF processing completed for URL ${url}`);
            res.status(202).json({ key, message: 'GIF is being processed', url: `https://art.cider.sh/artwork/${key}.gif` });
        }).catch((err) => {
            logger.error(`Job ${jobId}: Error finishing processing for URL ${url} - ${err.message}`);
            res.status(500).send('Error processing the stream');
        });
    }).catch((err) => {
        logger.error(`Job ${jobId}: Error adding to the queue for URL ${url} - ${err.message}`);
        res.status(500).send('Error adding to the queue');
    });
});

// Route to retrieve GIF
app.get('/artwork/:key.gif', (req, res) => {
    const key = req.params.key;
    const gifPath = path.join(cacheDir, `${key}.gif`);

    // Set cache headers for Cloudflare
    const sevenDaysInSeconds = 7 * 24 * 60 * 60;
    const expiresDate = new Date(Date.now() + sevenDaysInSeconds * 1000).toUTCString();

    res.setHeader('Cache-Control', `public, max-age=${sevenDaysInSeconds}`);
    res.setHeader('Expires', expiresDate);

    if (fs.existsSync(gifPath)) {
        logger.info(`Retrieving GIF for key ${key}`);
        return res.sendFile(gifPath);
    } else {
        logger.warn(`GIF not found for key ${key}`);
        return res.status(404).send('GIF not found');
    }
});

app.listen(port, () => {
    logger.info(`Server is running on http://art.cider.sh/`);
});
