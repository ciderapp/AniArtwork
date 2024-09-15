// ============================================================================
//  IMPORTS AND SETUP
// ============================================================================

import express from 'express';
import { createRequire } from 'module';
const require = createRequire(import.meta.url);
import crypto from 'crypto';
import Queue from 'bull';
import path from 'path';
import sharp from 'sharp';
import fsSync from 'fs/promises';
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

// ============================================================================
//  DIRECTORY SETUP
// ============================================================================

const cacheDir = path.join(__dirname, 'cache');
const artistSquaresDir = path.join(cacheDir, 'artist-squares');
const icloudArtDir = path.join(cacheDir, 'icloud-art');
const animatedArtDir = path.join(cacheDir, 'animated-art');

const ensureDirectories = async () => {
    const directories = [cacheDir, artistSquaresDir, icloudArtDir, animatedArtDir];
    for (const dir of directories) {
        await fsSync.mkdir(dir, { recursive: true })
            .catch(err => logger.error(`Error creating directory ${dir}: ${err.message}`));
    }
};

// ============================================================================
//  LOGGING SETUP
// ============================================================================

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

// ============================================================================
//  UTILITY FUNCTIONS
// ============================================================================

const validateGIF = async (gifPath) => {
    try {
        await exec(`ffmpeg -v error -i ${gifPath} -f null -`);
        return true;
    } catch (error) {
        return false;
    }
};

const generateKey = (url) => crypto.createHash('md5').update(url).digest('hex');

const cleanAndMigrateCache = async () => {
    logger.info('Starting cache cleanup and migration...');
    try {
        const files = (await fsSync.readdir(cacheDir)).filter(file => file.endsWith('.gif'));

        for (const file of files) {
            const oldPath = path.join(cacheDir, file);
            const newPath = path.join(animatedArtDir, file);
            const isValid = await validateGIF(oldPath);
            if (!isValid) {
                logger.warn(`Invalid GIF found and removed: ${file}`);
                await fsSync.unlink(oldPath);
            } else {
                logger.info(`Valid GIF: ${file}`);
                await fsSync.rename(oldPath, newPath);
                logger.info(`Migrated GIF to new directory: ${file}`);
            }
        }
        logger.info('Cache cleanup and migration completed.');
    } catch (err) {
        logger.error(`Cache cleanup and migration error: ${err.message}`);
    }
};

// ============================================================================
//  QUEUE SETUP
// ============================================================================

const processQueue = new Queue('processQueue', {
    redis: {
        host: '10.10.79.15',
        port: 6379
    },
    limiter: {
        max: 5,
        duration: 1000
    }
});

processQueue.on('error', (error) => {
    logger.error(`Queue error: ${error.message}`);
});

processQueue.on('failed', (job, err) => {
    logger.error(`Job ${job.id} failed: ${err.message}`);
});

const iCloudArtQueue = new Queue('iCloudArtQueue', {
    redis: {
        host: '10.10.79.15',
        port: 6379
    },
    limiter: {
        max: 5,
        duration: 1000
    }
});

iCloudArtQueue.on('error', (error) => {
    logger.error(`iCloud Art Queue error: ${error.message}`);
});

iCloudArtQueue.on('failed', (job, err) => {
    logger.error(`iCloud Art Job ${job.id} failed: ${err.message}`);
});

const artistSquareQueue = new Queue('artistSquareQueue', {
    redis: {
        host: '10.10.79.15',
        port: 6379
    },
    limiter: {
        max: 5,
        duration: 1000
    }
});

artistSquareQueue.on('error', (error) => {
    logger.error(`Artist Square Queue error: ${error.message}`);
});

artistSquareQueue.on('failed', (job, err) => {
    logger.error(`Artist Square Job ${job.id} failed: ${err.message}`);
});

// ============================================================================
//  STREAM PROCESSING
// ============================================================================

const processStream = async (url, key, jobId) => {
    logger.info(`Job ${jobId}: Starting processing for URL ${url}`);
    return new Promise((resolve, reject) => {
        const tempGifPath = path.join(animatedArtDir, `${key}_temp.gif`);
        const finalGifPath = path.join(animatedArtDir, `${key}.gif`);

        ffmpeg(url)
            .inputOptions('-protocol_whitelist file,http,https,tcp,tls,crypto')
            .output(tempGifPath)
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
            .on('end', async () => {
                logger.info(`Job ${jobId}: Processing completed for URL ${url}`);
                try {
                    const stats = await fsSync.stat(tempGifPath);
                    if (stats.size > 0) {
                        await fsSync.rename(tempGifPath, finalGifPath);
                        resolve(finalGifPath);
                    } else {
                        throw new Error('Generated GIF file is empty');
                    }
                } catch (error) {
                    await fsSync.unlink(tempGifPath).catch(err => logger.warn(`Failed to delete temporary file: ${err.message}`));
                    reject(error);
                }
            })
            .on('error', async (err) => {
                logger.error(`Job ${jobId}: Error processing URL ${url} - ${err.message}`);
                await fsSync.unlink(tempGifPath).catch(err => logger.warn(`Failed to delete temporary file: ${err.message}`));
                reject(err);
            })
            .run();
    });
};

processQueue.process(3, async (job) => {
    const { url, key, jobId } = job.data;
    const gifPath = path.join(animatedArtDir, `${key}.gif`);

    logger.info(`Job ${jobId}: Starting job for URL ${url}`);

    if (fs.existsSync(gifPath)) {
        logger.info(`Job ${jobId}: GIF already exists for URL ${url}`);
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

// ============================================================================
//  ROUTES
// ============================================================================

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
    const gifPath = path.join(animatedArtDir, `${key}.gif`);
    const jobId = crypto.randomBytes(4).toString('hex');

    logger.info(`Job ${jobId}: Received to generate GIF for URL ${url}`);

    if (fs.existsSync(gifPath)) {
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

app.get('/artwork/:key.gif', (req, res) => {
    const key = req.params.key;
    const gifPath = path.join(animatedArtDir, `${key}.gif`);

    if (fs.existsSync(gifPath)) {
        const sevenDaysInSeconds = 7 * 24 * 60 * 60;
        const expiresDate = new Date(Date.now() + sevenDaysInSeconds * 1000).toUTCString();

        res.setHeader('Cache-Control', `public, max-age=${sevenDaysInSeconds}`);
        res.setHeader('Expires', expiresDate);
        
        logger.info(`Retrieving GIF for key ${key}`);
        return res.sendFile(gifPath);
    } else {
        logger.warn(`GIF not found for key ${key}`);
        return res.status(404).send('GIF not found');
    }
});

// ============================================================================
//  ARTIST SQUARE PROCESSING
// ============================================================================

const generateArtistSquareKey = (imageUrls) => {
    const combinedUrls = imageUrls.sort().join('');
    return crypto.createHash('md5').update(combinedUrls).digest('hex');
};

const createArtistSquare = async (imageUrls) => {
    const size = 500;
    const images = await Promise.all(imageUrls.map(async url => {
        const response = await fetch(url);
        const arrayBuffer = await response.arrayBuffer();
        return sharp(Buffer.from(arrayBuffer))
            .resize(size, size, { fit: 'cover' })
            .toBuffer();
    }));

    let composite;
    const background = { r: 0, g: 0, b: 0, alpha: 1 };

    if (images.length === 2) {
        composite = sharp({
            create: { width: size, height: size, channels: 4, background }
        })
        .composite([
            { input: await sharp(images[0]).resize(size / 2, size).toBuffer(), top: 0, left: 0 },
            { input: await sharp(images[1]).resize(size / 2, size).toBuffer(), top: 0, left: size / 2 }
        ]);
    } else if (images.length === 3) {
        composite = sharp({
            create: { width: size, height: size, channels: 4, background }
        })
        .composite([
            { input: await sharp(images[0]).resize(size, size / 2).toBuffer(), top: 0, left: 0 },
            { input: await sharp(images[1]).resize(size / 2, size / 2).toBuffer(), top: size / 2, left: 0 },
            { input: await sharp(images[2]).resize(size / 2, size / 2).toBuffer(), top: size / 2, left: size / 2 }
        ]);
    } else if (images.length === 4) {
        composite = sharp({
            create: { width: size, height: size, channels: 4, background }
        })
        .composite([
            { input: await sharp(images[0]).resize(size / 2, size / 2).toBuffer(), top: 0, left: 0 },
            { input: await sharp(images[1]).resize(size / 2, size / 2).toBuffer(), top: 0, left: size / 2 },
            { input: await sharp(images[2]).resize(size / 2, size / 2).toBuffer(), top: size / 2, left: 0 },
            { input: await sharp(images[3]).resize(size / 2, size / 2).toBuffer(), top: size / 2, left: size / 2 }
        ]);
    } else {
        throw new Error('Invalid number of images. Must be 2, 3, or 4.');
    }

    return composite.jpeg().toBuffer();
};

artistSquareQueue.process(3, async (job) => {
    const { imageUrls, key, jobId } = job.data;
    const squarePath = path.join(artistSquaresDir, `${key}.jpg`);

    logger.info(`Job ${jobId}: Starting artist square job for ${imageUrls.length} images`);

    if (fs.existsSync(squarePath)) {
        logger.info(`Job ${jobId}: Artist square already exists for key ${key}`);
        return squarePath;
    }

    try {
        const squareBuffer = await createArtistSquare(imageUrls);
        await fsSync.writeFile(squarePath, squareBuffer);
        logger.info(`Job ${jobId}: Artist square created for key ${key}`);
        return squarePath;
    } catch (error) {
        logger.error(`Job ${jobId}: Error creating artist square - ${error.message}`);
        throw new Error('Error creating artist square');
    }
});

app.post('/artwork/artist-square', express.json(), async (req, res) => {
    const imageUrls = req.body.imageUrls;
    
    if (!Array.isArray(imageUrls) || imageUrls.length < 2 || imageUrls.length > 4) {
        return res.status(400).send('Invalid input. Provide 2-4 image URLs.');
    }
  
    const key = generateArtistSquareKey(imageUrls);
    const squarePath = path.join(artistSquaresDir, `${key}.jpg`);
    const jobId = crypto.randomBytes(4).toString('hex');
  
    if (fs.existsSync(squarePath)) {
        logger.info(`Job ${jobId}: Artist square already exists for key ${key}`);
        return res.status(200).json({ key, message: 'Artist square already exists', url: `https://art.cider.sh/artwork/artist-square/${key}.jpg` });
    }
  
    try {
        const job = await artistSquareQueue.add({ imageUrls, key, jobId });
        logger.info(`Job ${jobId}: Added to the artist square queue`);
  
        job.finished().then(() => {
            const sevenDaysInSeconds = 7 * 24 * 60 * 60;
            const expiresDate = new Date(Date.now() + sevenDaysInSeconds * 1000).toUTCString();
  
            res.setHeader('Cache-Control', `public, max-age=${sevenDaysInSeconds}`);
            res.setHeader('Expires', expiresDate);
  
            logger.info(`Job ${jobId}: Artist square processing completed`);
            res.status(202).json({ key, message: 'Artist square is being processed', url: `https://art.cider.sh/artwork/artist-square/${key}.jpg` });
        }).catch((err) => {
            logger.error(`Job ${jobId}: Error finishing processing - ${err.message}`);
            res.status(500).send('Error processing the artist square');
        });
    } catch (error) {
        logger.error(`Job ${jobId}: Error adding to the queue - ${error.message}`);
        res.status(500).send('Error adding to the queue');
    }
});

app.get('/artwork/artist-square/:key.jpg', (req, res) => {
    const key = req.params.key;
    const squarePath = path.join(artistSquaresDir, `${key}.jpg`);
  
    if (fs.existsSync(squarePath)) {
        const sevenDaysInSeconds = 7 * 24 * 60 * 60;
        const expiresDate = new Date(Date.now() + sevenDaysInSeconds * 1000).toUTCString();
  
        res.setHeader('Cache-Control', `public, max-age=${sevenDaysInSeconds}`);
        res.setHeader('Expires', expiresDate);
      
        logger.info(`Retrieving artist square for key ${key}`);
        return res.sendFile(squarePath);
    } else {
        logger.warn(`Artist square not found for key ${key}`);
        return res.status(404).send('Artist square not found');
    }
});

// ============================================================================
//  iCLOUD ART PROCESSING
// ============================================================================

const generateiCloudArtKey = (imageUrl) => {
    return crypto.createHash('md5').update(imageUrl).digest('hex');
};

const createiCloudArt = async (imageUrl) => {
    const size = 1024;
    const image = await fetch(imageUrl).then(response => response.arrayBuffer());
  
    const icloudImage = sharp(image)
        .resize(size, size, { fit: 'cover' });
  
    return icloudImage.jpeg().toBuffer();
};

iCloudArtQueue.process(3, async (job) => {
    const { imageUrl, key, jobId } = job.data;
    const icloudPath = path.join(icloudArtDir, `${key}.jpg`);

    logger.info(`Job ${jobId}: Starting iCloud Art job for ${imageUrl}`);

    if (fs.existsSync(icloudPath)) {
        logger.info(`Job ${jobId}: iCloud Art already exists for key ${key}`);
        return icloudPath;
    }

    try {
        const cloudBuffer = await createiCloudArt(imageUrl);
        await fsSync.writeFile(icloudPath, cloudBuffer);
        logger.info(`Job ${jobId}: iCloud Art created for key ${key}`);
        return icloudPath;
    } catch (error) {
        logger.error(`Job ${jobId}: Error creating iCloud Art - ${error.message}`);
        throw new Error('Error creating iCloud Art');
    }
});

app.post('/artwork/icloud', express.json(), async (req, res) => {
    const imageUrl = req.body.imageUrl;
  
    if (!imageUrl) {
        return res.status(400).send('Invalid input. Provide an image URL.');
    }
  
    const key = generateiCloudArtKey(imageUrl);
    const squarePath = path.join(icloudArtDir, `${key}.jpg`);
    const jobId = crypto.randomBytes(4).toString('hex');
  
    if (fs.existsSync(squarePath)) {
        logger.info(`Job ${jobId}: iCloud Art already exists for key ${key}`);
        return res.status(200).json({ key, message: 'iCloud Art already exists', url: `https://art.cider.sh/artwork/icloud/${key}.jpg` });
    }
  
    try {
        const job = await iCloudArtQueue.add({ imageUrl, key, jobId });
        logger.info(`Job ${jobId}: Added to the iCloud Art queue`);
  
        job.finished().then(() => {
            const sevenDaysInSeconds = 7 * 24 * 60 * 60;
            const expiresDate = new Date(Date.now() + sevenDaysInSeconds * 1000).toUTCString();
  
            res.setHeader('Cache-Control', `public, max-age=${sevenDaysInSeconds}`);
            res.setHeader('Expires', expiresDate);
  
            logger.info(`Job ${jobId}: iCloud Art processing completed`);
            res.status(202).json({ key, message: 'iCloud Art is being processed', url: `https://art.cider.sh/artwork/icloud/${key}.jpg` });
        }).catch((err) => {
            logger.error(`Job ${jobId}: Error finishing processing - ${err.message}`);
            res.status(500).send('Error processing the iCloud Art');
        });
    } catch (error) {
        logger.error(`Job ${jobId}: Error adding to the queue - ${error.message}`);
        res.status(500).send('Error adding to the queue');
    }
});

app.get('/artwork/icloud/:key.jpg', (req, res) => {
    const key = req.params.key;
    const iCloudPath = path.join(icloudArtDir, `${key}.jpg`);

    if (fs.existsSync(iCloudPath)) {
        const sevenDaysInSeconds = 7 * 24 * 60 * 60;
        const expiresDate = new Date(Date.now() + sevenDaysInSeconds * 1000).toUTCString();
    
        res.setHeader('Cache-Control', `public, max-age=${sevenDaysInSeconds}`);
        res.setHeader('Expires', expiresDate);
        
        logger.info(`Retrieving iCloud Art for key ${key}`);
        return res.sendFile(iCloudPath);
    } else {
        logger.warn(`iCloud Art not found for key ${key}`);
        return res.status(404).send('iCloud Art not found');
    }
});

// ============================================================================
//  SERVER STARTUP
// ============================================================================

ensureDirectories()
    // Disable migration, as it's not needed anymore
    // .then(() => cleanAndMigrateCache())
    .then(() => {
        app.listen(port, () => {
            logger.info(`Server is running on http://art.cider.sh/`);
        });
    })
    .catch(err => {
        logger.error(`Startup error: ${err.message}`);
        process.exit(1);
    });
