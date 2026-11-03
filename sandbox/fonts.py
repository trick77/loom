"""Turn the UI's variable sans (woff2) into static TTFs matplotlib can read.

matplotlib only scans ttf/otf and its FreeType has no woff2 support. A variable
font would render its default instance only, so each weight becomes its own
file, all under the family the UI uses ("Anthropic Sans"). Runs in a build
stage of sandbox/Containerfile: python fonts.py <in.woff2> <outdir>
"""

import os
import sys

from fontTools.ttLib import TTFont
from fontTools.varLib.instancer import instantiateVariableFont

FAMILY = "Anthropic Sans"
# Every weight the charts ask for gets its exact file: a nearest-weight match
# makes matplotlib log a findfont warning into the job's output.
WEIGHTS = {400: "Regular", 600: "SemiBold", 700: "Bold"}


def main(src, outdir):
    os.makedirs(outdir, exist_ok=True)
    for weight, style in WEIGHTS.items():
        font = instantiateVariableFont(TTFont(src), {"wght": weight, "opsz": 16})
        font.flavor = None
        font["OS/2"].usWeightClass = weight
        name = font["name"]
        for nid in (16, 17, 21, 22):
            name.removeNames(nameID=nid)
        ps = f"AnthropicSans-{style}"
        for nid, value in ((1, FAMILY), (2, style), (3, ps), (4, f"{FAMILY} {style}"), (6, ps)):
            name.setName(value, nid, 3, 1, 0x409)
            name.setName(value, nid, 1, 0, 0)
        font.save(os.path.join(outdir, f"{ps}.ttf"))


if __name__ == "__main__":
    main(sys.argv[1], sys.argv[2])
