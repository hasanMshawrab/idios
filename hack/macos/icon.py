#!/usr/bin/env python3
"""Generate the idios app icon and menu bar glyph assets.

Usage: hack/macos/icon.py

The icon is four shapes -- a graphite slab, a red tittle and a stack of
records -- so the geometry lives here rather than in hand-edited artwork:
the 32pt and 16pt steps are not downscales of the 1024pt drawing but
separate reductions (three records, then two, then one), and only a
generator can keep the three in step. Writes the SVG master under
macos/icon/ and every PNG the asset catalogs need.

Needs ImageMagick (magick) for rasterizing. Its bundled SVG renderer
drops gradients, so the slab gradient is composited here instead of
declared in the SVG.
"""

import math
import os
import shutil
import subprocess
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
ICON_DIR = os.path.join(ROOT, "macos", "icon")
ASSETS = os.path.join(ROOT, "macos", "idios", "Assets.xcassets")
APPICON = os.path.join(ASSETS, "AppIcon.appiconset")
GLYPH = os.path.join(ASSETS, "MenuBarGlyph.imageset")

# Everything is laid out on a 1024 unit canvas. The slab is 824 units
# centred in it, which is the macOS app icon grid: the 100 units of
# padding on every side is where the system draws the icon's shadow.
CANVAS = 1024
SLAB = 824
SLAB_MIN = (CANVAS - SLAB) / 2
SLAB_MAX = SLAB_MIN + SLAB

# A superellipse of this exponent is the macOS squircle to within a
# fraction of a unit at 1024, and unlike an `rx` rounded rectangle it has
# no visible curvature break where the corner meets the edge.
SQUIRCLE_EXPONENT = 5.0
SQUIRCLE_POINTS = 720

SLAB_TOP = "#2c313a"
SLAB_BOTTOM = "#12151a"
SLAB_EDGE = "#ffffff"
SLAB_EDGE_ALPHA = 0.16
RECORD = "#f2f2f7"
TITTLE = "#ff453a"

# One entry per reduction: the tittle, then the records as (y, height,
# opacity). Keyed by how many records the mark carries.
MARKS = {
    3: {
        "tittle": (512, 310, 78),
        "column": (420, 184, 32),
        "records": [(432, 106, 1.0), (560, 106, 0.82), (688, 106, 0.64)],
    },
    2: {
        "tittle": (512, 316, 88),
        "column": (408, 208, 40),
        "records": [(452, 160, 1.0), (636, 160, 0.78)],
    },
    1: {
        "tittle": (512, 290, 116),
        "column": (388, 248, 60),
        "records": [(448, 392, 1.0)],
    },
}

# The menu bar extra is a template image: macOS keeps only the alpha and
# the app tints the whole glyph, so the glyph is the one-record reduction
# with no slab, drawn to fill the canvas instead of sitting inside one.
GLYPH_MARK = {
    "tittle": (512, 210, 132),
    "column": (372, 280, 68),
    "records": [(410, 536, 1.0)],
}

# Every pixel size the app icon needs, and the reduction each one uses.
# 64 is the smallest size that still holds three records; 16 holds one.
ICON_SIZES = {16: 1, 32: 2, 64: 3, 128: 3, 256: 3, 512: 3, 1024: 3}

# The ten slots a mac app icon set declares, in points. Every slot needs a
# filename of its own even where two of them are the same pixel size (16pt
# at 2x and 32pt at 1x are both 32px): actool keeps one slot per filename
# and silently drops the rest, which costs the .icns its large
# representations and leaves the Dock on the generic tile.
APPICON_SLOTS = [(points, scale) for points in (16, 32, 128, 256, 512) for scale in (1, 2)]
GLYPH_SIZES = [16, 32]


def slot_filename(points, scale):
    return "idios-%d%s.png" % (points, "" if scale == 1 else "@2x")


def squircle_path(lo, hi, exponent=SQUIRCLE_EXPONENT, points=SQUIRCLE_POINTS):
    """squircle_path returns an SVG path for the superellipse spanning lo..hi."""
    radius = (hi - lo) / 2
    centre = lo + radius
    coords = []
    for i in range(points):
        theta = 2 * math.pi * i / points
        cos, sin = math.cos(theta), math.sin(theta)
        x = centre + radius * math.copysign(abs(cos) ** (2 / exponent), cos)
        y = centre + radius * math.copysign(abs(sin) ** (2 / exponent), sin)
        coords.append("%.3f %.3f" % (x, y))
    return "M" + " L".join(coords) + " Z"


def svg(body):
    return (
        '<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" '
        'viewBox="0 0 %d %d">\n%s\n</svg>\n' % (CANVAS, CANVAS, CANVAS, CANVAS, body)
    )


def mark_shapes(mark, record_colour=RECORD, tittle_colour=TITTLE):
    cx, cy, r = mark["tittle"]
    x, width, rx = mark["column"]
    shapes = [
        '  <circle cx="%g" cy="%g" r="%g" fill="%s"/>' % (cx, cy, r, tittle_colour)
    ]
    for y, height, opacity in mark["records"]:
        shapes.append(
            '  <rect x="%g" y="%g" width="%g" height="%g" rx="%g" fill="%s" '
            'fill-opacity="%g"/>' % (x, y, width, height, rx, record_colour, opacity)
        )
    return "\n".join(shapes)


def slab_svg():
    """slab_svg is the white mask the slab gradient is clipped to."""
    path = squircle_path(SLAB_MIN, SLAB_MAX)
    return svg('  <path d="%s" fill="#ffffff"/>' % path)


def edge_svg():
    """edge_svg is the hairline that keeps the dark slab off a dark Dock."""
    path = squircle_path(SLAB_MIN + 2, SLAB_MAX - 2)
    return svg(
        '  <path d="%s" fill="none" stroke="%s" stroke-opacity="%g" '
        'stroke-width="4"/>' % (path, SLAB_EDGE, SLAB_EDGE_ALPHA)
    )


def master_svg():
    """master_svg is the committed 1024pt drawing, readable in a browser."""
    path = squircle_path(SLAB_MIN, SLAB_MAX)
    inner = squircle_path(SLAB_MIN + 2, SLAB_MAX - 2)
    body = "\n".join(
        [
            "  <defs>",
            '    <linearGradient id="slab" x1="0" y1="0" x2="0" y2="1">',
            '      <stop offset="0" stop-color="%s"/>' % SLAB_TOP,
            '      <stop offset="1" stop-color="%s"/>' % SLAB_BOTTOM,
            "    </linearGradient>",
            "  </defs>",
            '  <path d="%s" fill="url(#slab)"/>' % path,
            '  <path d="%s" fill="none" stroke="%s" stroke-opacity="%g" '
            'stroke-width="4"/>' % (inner, SLAB_EDGE, SLAB_EDGE_ALPHA),
            mark_shapes(MARKS[3]),
        ]
    )
    return svg(body)


def glyph_svg():
    """glyph_svg is the menu bar template: one colour, alpha carries it all."""
    return svg(mark_shapes(GLYPH_MARK, record_colour="#000000", tittle_colour="#000000"))


def run(args):
    subprocess.run(args, check=True, capture_output=True)


def rasterize(source, target):
    """rasterize renders an SVG at the canvas size it declares.

    Never pass magick a -size for an SVG: its bundled renderer crops the
    drawing to that box instead of scaling it to fit, which silently
    yields a blank PNG for every size smaller than the artwork.
    """
    run(["magick", "-background", "none", source, target])


def resize(source, size, target):
    run(["magick", source, "-filter", "Lanczos", "-resize", "%dx%d" % (size, size), target])


def build_master(records, work):
    """build_master composites gradient, mask, hairline and mark at 1024."""
    target = os.path.join(work, "icon-%d.png" % records)
    slab = os.path.join(work, "slab.png")
    edge = os.path.join(work, "edge.png")
    mark = os.path.join(work, "mark-%d.png" % records)
    grad = os.path.join(work, "grad.png")
    rasterize(os.path.join(work, "slab.svg"), slab)
    rasterize(os.path.join(work, "edge.svg"), edge)
    rasterize(os.path.join(work, "mark-%d.svg" % records), mark)
    run(
        [
            "magick",
            "-size",
            "%dx%d" % (CANVAS, CANVAS),
            "gradient:%s-%s" % (SLAB_TOP, SLAB_BOTTOM),
            grad,
        ]
    )
    run(["magick", grad, slab, "-compose", "CopyOpacity", "-composite", target])
    run(["magick", target, edge, "-composite", mark, "-composite", target])
    return target


def appicon_contents():
    entries = []
    for points, scale in APPICON_SLOTS:
        entries.append(
            "    {\n"
            '      "filename" : "%s",\n'
            '      "idiom" : "mac",\n'
            '      "scale" : "%dx",\n'
            '      "size" : "%dx%d"\n'
            "    }" % (slot_filename(points, scale), scale, points, points)
        )
    return (
        '{\n  "images" : [\n'
        + ",\n".join(entries)
        + '\n  ],\n  "info" : {\n    "author" : "xcode",\n    "version" : 1\n  }\n}\n'
    )


def glyph_contents():
    entries = []
    for scale in (1, 2):
        entries.append(
            "    {\n"
            '      "filename" : "menubar-%d.png",\n'
            '      "idiom" : "universal",\n'
            '      "scale" : "%dx"\n'
            "    }" % (16 * scale, scale)
        )
    return (
        '{\n  "images" : [\n'
        + ",\n".join(entries)
        + '\n  ],\n  "info" : {\n    "author" : "xcode",\n    "version" : 1\n  },\n'
        '  "properties" : {\n    "template-rendering-intent" : "template"\n  }\n}\n'
    )


def mark_shapes_svg(records):
    return svg(mark_shapes(MARKS[records]))


def write(path, text):
    with open(path, "w") as handle:
        handle.write(text)


def main():
    if subprocess.run(["which", "magick"], capture_output=True).returncode != 0:
        sys.exit("icon.py needs ImageMagick: brew install imagemagick")

    for directory in (ICON_DIR, APPICON, GLYPH):
        os.makedirs(directory, exist_ok=True)
    work = os.path.join(ICON_DIR, "build")
    os.makedirs(work, exist_ok=True)

    write(os.path.join(work, "slab.svg"), slab_svg())
    write(os.path.join(work, "edge.svg"), edge_svg())
    for records in MARKS:
        write(os.path.join(work, "mark-%d.svg" % records), mark_shapes_svg(records))
    write(os.path.join(work, "glyph.svg"), glyph_svg())
    write(os.path.join(ICON_DIR, "idios.svg"), master_svg())

    masters = {records: build_master(records, work) for records in MARKS}
    rendered = {}
    for size in sorted(ICON_SIZES):
        rendered[size] = os.path.join(work, "render-%d.png" % size)
        if size == CANVAS:
            run(["magick", masters[ICON_SIZES[size]], rendered[size]])
        else:
            resize(masters[ICON_SIZES[size]], size, rendered[size])

    for name in os.listdir(APPICON):
        if name.endswith(".png"):
            os.remove(os.path.join(APPICON, name))
    for points, scale in APPICON_SLOTS:
        shutil.copyfile(
            rendered[points * scale],
            os.path.join(APPICON, slot_filename(points, scale)),
        )
    write(os.path.join(APPICON, "Contents.json"), appicon_contents())

    glyph = os.path.join(work, "glyph.png")
    rasterize(os.path.join(work, "glyph.svg"), glyph)
    for size in GLYPH_SIZES:
        resize(glyph, size, os.path.join(GLYPH, "menubar-%d.png" % size))
    write(os.path.join(GLYPH, "Contents.json"), glyph_contents())

    for name in os.listdir(work):
        os.remove(os.path.join(work, name))
    os.rmdir(work)
    print("wrote macos/icon/idios.svg, %d app icon PNGs, %d glyph PNGs"
          % (len(APPICON_SLOTS), len(GLYPH_SIZES)))


if __name__ == "__main__":
    main()
