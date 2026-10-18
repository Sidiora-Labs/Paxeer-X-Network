import sys
from build import TARGETS
if len(sys.argv)!=3 or TARGETS.get(sys.argv[1])!=sys.argv[2]: raise SystemExit("unrecognized service/binary")
