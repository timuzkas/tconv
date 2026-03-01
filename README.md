# tconv

convert images and videos without the headache

tconv is a simple gtk4 converter app that handles images and videos with a clean drag-and-drop interface.

## install

```
go install github.com/timuzkas/tconv@latest
```

or clone and `go build`

Linux build available in Releases!

## usage

```
tconv
```

drag files onto the source box, pick your format and quality, hit convert. you can also ctrl+o to open files.

## features

- images: png, jpg, webp, gif, avif, bmp, tiff
- videos: mp4, webm, gif, mov, mkv
- quality slider with live size estimation
- strip metadata option
- batch conversion
- output to source folder or zip

## depends

- imagemagick (for images)
- ffmpeg (for videos)
- gtk4
