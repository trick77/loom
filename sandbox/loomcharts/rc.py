"""Write loom's chart style as a matplotlibrc: python -m loomcharts.rc <path>.

The image build installs it in the matplotlib config every job copies, so a
plain matplotlib chart comes out in loom's style without apply_style().
"""

import os
import sys

from ._style import matplotlibrc


def main(argv):
    path = argv[0]
    os.makedirs(os.path.dirname(path) or ".", exist_ok=True)
    with open(path, "w", encoding="utf-8") as f:
        f.write(matplotlibrc())
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
