from importlib.util import module_from_spec, spec_from_file_location
from pathlib import Path

_spec = spec_from_file_location('kindle_topology_v1', Path(__file__).parents[1] / 'topology.py')
v1 = module_from_spec(_spec)
_spec.loader.exec_module(v1)
