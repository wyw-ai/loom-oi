package filesystem

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"image"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io"
	"math"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

type cappedBuffer struct {
	bytes.Buffer
	max int
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.max-b.Len() {
		return 0, errors.New("image candidate exceeds limit")
	}
	return b.Buffer.Write(p)
}
func encode(img image.Image, quality, limit int) []byte {
	b := cappedBuffer{max: limit}
	var err error
	if quality == 0 {
		err = png.Encode(&b, img)
	} else {
		err = jpeg.Encode(&b, img, &jpeg.Options{Quality: quality})
	}
	if err != nil {
		return nil
	}
	return b.Bytes()
}

func imageInfo(format string, b []byte) (int, *Failure) {
	orient := 1
	set := func(x []byte) {
		if v := exifOrientation(x); v != 0 {
			orient = v
		}
	}
	switch format {
	case "png":
		for at := 8; at+12 <= len(b); {
			n := int(binary.BigEndian.Uint32(b[at:]))
			if n > len(b)-at-12 {
				return 0, fail("INVALID_IMAGE", "invalid PNG chunk length")
			}
			kind := string(b[at+4 : at+8])
			if kind == "acTL" {
				return 0, fail("UNSUPPORTED_IMAGE", "animated PNG is unsupported")
			}
			if kind == "eXIf" {
				set(b[at+8 : at+8+n])
			}
			at += 12 + n
		}
	case "webp":
		for at := 12; at+8 <= len(b); {
			n := int(binary.LittleEndian.Uint32(b[at+4:]))
			if n > len(b)-at-8 {
				return 0, fail("INVALID_IMAGE", "invalid WebP chunk length")
			}
			kind := string(b[at : at+4])
			if kind == "ANIM" || kind == "ANMF" || kind == "VP8X" && n > 0 && b[at+8]&2 != 0 {
				return 0, fail("UNSUPPORTED_IMAGE", "animated WebP is unsupported")
			}
			if kind == "EXIF" {
				set(b[at+8 : at+8+n])
			}
			at += 8 + n + n%2
		}
	case "jpeg":
		for at := 2; at < len(b) && b[at] == 0xff; {
			for at < len(b) && b[at] == 0xff {
				at++
			}
			if at >= len(b) {
				break
			}
			marker := b[at]
			at++
			if marker == 0xda || marker == 0xd9 {
				break
			}
			if marker == 0x01 || marker >= 0xd0 && marker <= 0xd7 {
				continue
			}
			if at+2 > len(b) {
				break
			}
			n := int(binary.BigEndian.Uint16(b[at:]))
			if n < 2 || n > len(b)-at {
				return 0, fail("INVALID_IMAGE", "invalid JPEG segment length")
			}
			if marker == 0xe1 && bytes.HasPrefix(b[at+2:at+n], []byte("Exif\x00\x00")) {
				set(b[at+2 : at+n])
			}
			at += n
		}
	default:
		return 0, fail("UNSUPPORTED_IMAGE", "expected static PNG, JPEG or WebP")
	}
	return orient, nil
}
func exifOrientation(raw []byte) int {
	b := bytes.TrimPrefix(raw, []byte("Exif\x00\x00"))
	if len(b) < 8 {
		return 0
	}
	var order binary.ByteOrder
	switch string(b[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 0
	}
	if order.Uint16(b[2:]) != 42 {
		return 0
	}
	off := int(order.Uint32(b[4:]))
	if off < 8 || off > len(b)-2 {
		return 0
	}
	n := int(order.Uint16(b[off:]))
	for i := 0; i < n; i++ {
		p := off + 2 + i*12
		if p+12 > len(b) {
			return 0
		}
		if order.Uint16(b[p:]) == 274 && order.Uint16(b[p+2:]) == 3 && order.Uint32(b[p+4:]) == 1 {
			v := int(order.Uint16(b[p+8:]))
			if v >= 1 && v <= 8 {
				return v
			}
		}
	}
	return 0
}
func orientImage(ctx context.Context, im image.Image, o int) (image.Image, *Failure) {
	if o == 1 {
		return im, nil
	}
	b := im.Bounds()
	w, h := b.Dx(), b.Dy()
	dw, dh := w, h
	if o >= 5 {
		dw, dh = h, w
	}
	var dst draw.Image
	switch im.(type) {
	case *image.NRGBA64, *image.RGBA64, *image.Gray16:
		dst = image.NewNRGBA64(image.Rect(0, 0, dw, dh))
	default:
		dst = image.NewNRGBA(image.Rect(0, 0, dw, dh))
	}
	for y := 0; y < h; y++ {
		if e := check(ctx); e != nil {
			return nil, e
		}
		for x := 0; x < w; x++ {
			dx, dy := x, y
			switch o {
			case 2:
				dx = w - 1 - x
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dy = h - 1 - y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			case 8:
				dx, dy = y, w-1-x
			}
			dst.Set(dx, dy, im.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst, nil
}
func (s *Service) image(ctx context.Context, q Request, p string) (Object, *Failure) {
	f, st, e := s.ordinary(p)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	maxSrc := s.images["max_source_bytes"]
	if st.Size() > int64(maxSrc) {
		return nil, fail("FILE_TOO_LARGE", "encoded source exceeds limit")
	}
	raw, err := io.ReadAll(io.LimitReader(f, int64(maxSrc)+1))
	if err != nil {
		return nil, ioError(err)
	}
	if len(raw) > maxSrc {
		return nil, fail("FILE_TOO_LARGE", "encoded source exceeds limit")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		if errors.Is(err, image.ErrFormat) {
			return nil, fail("UNSUPPORTED_IMAGE", "unknown or unsupported image format")
		}
		return nil, fail("INVALID_IMAGE", "invalid image header")
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || max(cfg.Width, cfg.Height) > s.images["max_dimension"] || cfg.Width > s.images["max_pixels"]/cfg.Height {
		return nil, fail("FILE_TOO_LARGE", "source dimensions exceed image budget")
	}
	o, e := imageInfo(format, raw)
	if e != nil {
		return nil, e
	}
	if e := check(ctx); e != nil {
		return nil, e
	}
	im, actual, err := image.Decode(bytes.NewReader(raw))
	if err != nil || actual != format || im.Bounds().Dx() != cfg.Width || im.Bounds().Dy() != cfg.Height {
		return nil, fail("INVALID_IMAGE", "could not decode image")
	}
	im, e = orientImage(ctx, im, o)
	if e != nil {
		return nil, e
	}
	w, h := im.Bounds().Dx(), im.Bounds().Dy()
	crop := object(q.Input, "crop")
	var cropMeta any
	if crop != nil {
		cropMeta = crop
	}
	sourceMime := "image/" + format
	hard := s.images["max_encoded_bytes"]
	if hard <= 0 {
		hard = s.FrameLimit() * 3 / 4
	}
	target := min(s.images["target_encoded_bytes"], hard)
	maxEdge := s.images["max_preview_edge"]
	tooBig := func() (Object, *Failure) {
		return nil, fail("IMAGE_PREVIEW_TOO_LARGE", "select a smaller crop to preserve detail")
	}
	pack := func(b []byte, mime string, pw, ph int, resized, reencoded bool) Object {
		if len(b) == 0 || len(b) > hard || maxEdge > 0 && max(pw, ph) > maxEdge {
			return nil
		}
		meta := Object{"path": p, "source_mime": sourceMime, "source_width": w, "source_height": h, "orientation_applied": o != 1, "crop": cropMeta, "preview_width": pw, "preview_height": ph, "resized": resized, "reencoded": reencoded, "mime_type": mime, "encoded_bytes": len(b)}
		slot := Object{"type": "image", "mimeType": mime, "data": ""}
		out := Object{"content": []Object{slot}, "structuredContent": meta}
		if frameSize(q.ID, "content.v1", out)+base64.StdEncoding.EncodedLen(len(b)) > s.FrameLimit() {
			return nil
		}
		slot["data"] = base64.StdEncoding.EncodeToString(b)
		return out
	}
	if crop != nil {
		x, y := num(crop, "x", 0), num(crop, "y", 0)
		cw, ch := num(crop, "width", 0), num(crop, "height", 0)
		if x < 0 || y < 0 || cw <= 0 || ch <= 0 || cw > w || ch > h || x > w-cw || y > h-ch {
			return nil, fail("CROP_OUT_OF_BOUNDS", "crop is outside oriented image")
		}
		if maxEdge > 0 && max(cw, ch) > maxEdge {
			return tooBig()
		}
		if e := check(ctx); e != nil {
			return nil, e
		}
		var region image.Image
		r := image.Rect(x, y, x+cw, y+ch).Add(im.Bounds().Min)
		if sub, ok := im.(interface {
			SubImage(image.Rectangle) image.Image
		}); ok {
			region = sub.SubImage(r)
		} else {
			dst := image.NewNRGBA64(image.Rect(0, 0, cw, ch))
			draw.Draw(dst, dst.Bounds(), im, r.Min, draw.Src)
			region = dst
		}
		if out := pack(encode(region, 0, hard), "image/png", cw, ch, false, true); out != nil {
			return out, nil
		}
		return tooBig()
	}
	if o == 1 {
		if out := pack(raw, sourceMime, w, h, false, false); out != nil {
			return out, nil
		}
	}
	edge := max(w, h)
	if maxEdge > 0 {
		edge = min(edge, maxEdge)
	}
	minEdge := min(max(w, h), s.images["min_preview_edge"])
	for {
		if e := check(ctx); e != nil {
			return nil, e
		}
		pw := max(1, int(math.Round(float64(w)*float64(edge)/float64(max(w, h)))))
		ph := max(1, int(math.Round(float64(h)*float64(edge)/float64(max(w, h)))))
		resized := pw != w || ph != h
		candidate := im
		if resized {
			dst := image.NewNRGBA(image.Rect(0, 0, pw, ph))
			xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), im, im.Bounds(), draw.Src, nil)
			candidate = dst
		}
		if out := pack(encode(candidate, 0, hard), "image/png", pw, ph, resized, true); out != nil {
			return out, nil
		}
		var best Object
		if opaque, ok := candidate.(interface{ Opaque() bool }); ok && opaque.Opaque() {
			for _, quality := range []int{90, 85, 80, 75} {
				if e := check(ctx); e != nil {
					return nil, e
				}
				b := encode(candidate, quality, hard)
				if out := pack(b, "image/jpeg", pw, ph, resized, true); out != nil {
					if len(b) <= target {
						return out, nil
					}
					if best == nil {
						best = out
					}
				}
			}
		}
		if best != nil {
			return best, nil
		}
		if edge <= minEdge {
			return tooBig()
		}
		edge = max(minEdge, int(float64(edge)*0.75))
	}
}
