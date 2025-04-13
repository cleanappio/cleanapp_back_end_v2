"use strict";

Object.defineProperty(exports, "__esModule", {
  value: true
});
exports.default = void 0;
var _react = _interopRequireWildcard(require("react"));
var _CircleLayer = _interopRequireDefault(require("./CircleLayer"));
var _RasterLayer = _interopRequireDefault(require("./RasterLayer"));
var _SymbolLayer = require("./SymbolLayer");
var _LineLayer = _interopRequireDefault(require("./LineLayer"));
var _FillLayer = _interopRequireDefault(require("./FillLayer"));
var _FillExtrusionLayer = _interopRequireDefault(require("./FillExtrusionLayer"));
var _BackgroundLayer = _interopRequireDefault(require("./BackgroundLayer"));
var _HeatmapLayer = _interopRequireDefault(require("./HeatmapLayer"));
var _VectorSource = _interopRequireDefault(require("./VectorSource"));
var _RasterSource = _interopRequireDefault(require("./RasterSource"));
var _ImageSource = _interopRequireDefault(require("./ImageSource"));
var _ShapeSource = require("./ShapeSource");
var _jsxRuntime = require("react/jsx-runtime");
function _interopRequireDefault(e) { return e && e.__esModule ? e : { default: e }; }
function _getRequireWildcardCache(e) { if ("function" != typeof WeakMap) return null; var r = new WeakMap(), t = new WeakMap(); return (_getRequireWildcardCache = function (e) { return e ? t : r; })(e); }
function _interopRequireWildcard(e, r) { if (!r && e && e.__esModule) return e; if (null === e || "object" != typeof e && "function" != typeof e) return { default: e }; var t = _getRequireWildcardCache(r); if (t && t.has(e)) return t.get(e); var n = { __proto__: null }, a = Object.defineProperty && Object.getOwnPropertyDescriptor; for (var u in e) if ("default" !== u && {}.hasOwnProperty.call(e, u)) { var i = a ? Object.getOwnPropertyDescriptor(e, u) : null; i && (i.get || i.set) ? Object.defineProperty(n, u, i) : n[u] = e[u]; } return n.default = e, t && t.set(e, n), n; }
function toCamelCase(s) {
  return s.replace(/([-_][a-z])/gi, $1 => {
    return $1.toUpperCase().replace('-', '').replace('_', '');
  });
}

// Patches the Mapbox Style Specification keys into the style props attributes:
// icon-allow-overlap -> iconAllowOverlap
function toCamelCaseKeys(oldObj) {
  if (!oldObj) {
    return {};
  }
  const newObj = {};
  Object.keys(oldObj).forEach(key => {
    const value = oldObj[key];
    if (key.includes('-')) {
      newObj[toCamelCase(key)] = value;
    } else {
      newObj[key] = value;
    }
  });
  return newObj;
}
function getLayerComponentType(layer) {
  const {
    type
  } = layer;
  switch (type) {
    case 'circle':
      return _CircleLayer.default;
    case 'symbol':
      return _SymbolLayer.SymbolLayer;
    case 'raster':
      return _RasterLayer.default;
    case 'line':
      return _LineLayer.default;
    case 'fill':
      return _FillLayer.default;
    case 'fill-extrusion':
      return _FillExtrusionLayer.default;
    case 'background':
      return _BackgroundLayer.default;
    case 'heatmap':
      return _HeatmapLayer.default;
  }
  console.warn(`Mapbox layer type '${type}' is not supported/`);
  return null;
}
function asLayerComponent(layer) {
  const LayerComponent = getLayerComponentType(layer);
  if (!LayerComponent) {
    return null;
  }
  const style = {
    ...toCamelCaseKeys(layer.paint),
    ...toCamelCaseKeys(layer.layout)
  };
  const layerProps = {};
  if (layer.source) {
    layerProps.sourceID = layer.source;
  }
  if (layer['source-layer']) {
    layerProps.sourceLayerID = layer['source-layer'];
  }
  if (layer.minzoom) {
    layerProps.minZoomLevel = layer.minzoom;
  }
  if (layer.maxzoom) {
    layerProps.maxZoomLevel = layer.maxzoom;
  }
  if (layer.filter) {
    layerProps.filter = layer.filter;
  }
  if (Object.keys(style).length) {
    layerProps.style = style;
  }
  return /*#__PURE__*/(0, _jsxRuntime.jsx)(LayerComponent, {
    id: layer.id,
    ...layerProps
  }, layer.id);
}
function getTileSourceProps(source) {
  const sourceProps = {};
  if (source.url) {
    sourceProps.url = source.url;
  }
  if (source.tiles) {
    sourceProps.tileUrlTemplates = source.tiles;
  }
  if (source.minzoom !== undefined) {
    sourceProps.minZoomLevel = source.minzoom;
  }
  if (source.maxzoom !== undefined) {
    sourceProps.maxZoomLevel = source.maxzoom;
  }
  if (source.attribution) {
    sourceProps.attribution = source.attribution;
  }
  if (source.scheme && source.scheme === 'tms') {
    sourceProps.tms = true;
  }
  return sourceProps;
}
function getVectorSource(id, source) {
  const sourceProps = {
    ...getTileSourceProps(source)
  };
  return /*#__PURE__*/(0, _jsxRuntime.jsx)(_VectorSource.default, {
    id: id,
    ...sourceProps
  }, id);
}
function getRasterSource(id, source) {
  const sourceProps = {
    ...getTileSourceProps(source)
  };
  if (source.tileSize) {
    sourceProps.tileSize = source.tileSize;
  }
  return /*#__PURE__*/(0, _jsxRuntime.jsx)(_RasterSource.default, {
    id: id,
    ...sourceProps
  }, id);
}
function getImageSource(id, source) {
  const sourceProps = {
    url: source.url,
    coordinates: source.coordinates
  };
  return /*#__PURE__*/(0, _jsxRuntime.jsx)(_ImageSource.default, {
    id: id,
    ...sourceProps
  }, id);
}
function getShapeSource(id, source) {
  const sourceProps = {};
  if (source.data && typeof source.data === 'string') {
    sourceProps.url = source.data;
  } else if (source.data && typeof source.data === 'object') {
    sourceProps.shape = source.data;
  }
  if (source.cluster !== undefined) {
    sourceProps.cluster = source.cluster;
  }
  if (source.clusterRadius !== undefined) {
    sourceProps.clusterRadius = source.clusterRadius;
  }
  if (source.maxzoom !== undefined) {
    sourceProps.maxZoomLevel = source.maxzoom;
  }
  if (source.clusterMaxZoom !== undefined) {
    sourceProps.clusterMaxZoomLevel = source.clusterMaxZoom;
  }
  if (source.clusterProperties !== undefined) {
    sourceProps.clusterProperties = source.clusterProperties;
  }
  if (source.buffer !== undefined) {
    sourceProps.buffer = source.buffer;
  }
  if (source.tolerance !== undefined) {
    sourceProps.tolerance = source.tolerance;
  }
  if (source.lineMetrics !== undefined) {
    sourceProps.lineMetrics = source.lineMetrics;
  }
  return /*#__PURE__*/(0, _jsxRuntime.jsx)(_ShapeSource.ShapeSource, {
    id: id,
    ...sourceProps
  }, id);
}
function asSourceComponent(id, source) {
  switch (source.type) {
    case 'vector':
      return getVectorSource(id, source);
    case 'raster':
      return getRasterSource(id, source);
    case 'image':
      return getImageSource(id, source);
    case 'geojson':
      return getShapeSource(id, source);
  }
  console.warn(`Mapbox source type '${source.type}' is not supported/`);
  return null;
}
/**
 * Style is a component that automatically adds sources / layers to the map using Mapbox GL Style Spec.
 * Only [`sources`](https://docs.mapbox.com/mapbox-gl-js/style-spec/sources) & [`layers`](https://docs.mapbox.com/mapbox-gl-js/style-spec/layers/) are supported.
 * Other fields such as `sprites`, `glyphs` etc. will be ignored. Not all layer / source attributes from the style spec are supported, in general the supported attributes will be mentioned under https://github.com/rnmapbox/maps/tree/main/docs.
 */
const Style = props => {
  const [fetchedJson, setFetchedJson] = (0, _react.useState)({});
  const json = typeof props.json === 'object' ? props.json : fetchedJson;

  // Fetch style when props.json is a URL
  (0, _react.useEffect)(() => {
    const abortController = new AbortController();
    const fetchStyleJson = async json => {
      try {
        const response = await fetch(json, {
          signal: abortController.signal
        });
        const responseJson = await response.json();
        setFetchedJson(responseJson);
      } catch (error) {
        const e = error;
        if (e.name === 'AbortError') {
          return;
        }
        throw e;
      }
    };
    if (typeof props.json === 'string') {
      fetchStyleJson(props.json);
    }
    return function cleanup() {
      abortController.abort();
    };
  }, [props.json]);

  // Extract layer components from json
  const layerComponents = (0, _react.useMemo)(() => {
    if (!json.layers) {
      return [];
    }
    return json.layers.map(asLayerComponent).filter(x => !!x);
  }, [json.layers]);

  // Extract source components from json
  const {
    sources
  } = json;
  const sourceComponents = (0, _react.useMemo)(() => {
    if (!sources || !Object.keys(sources)) {
      return [];
    }
    return Object.keys(sources).map(id => asSourceComponent(id, sources[id])).filter(x => !!x);
  }, [sources]);
  return /*#__PURE__*/(0, _jsxRuntime.jsxs)(_jsxRuntime.Fragment, {
    children: [sourceComponents, layerComponents]
  });
};
var _default = exports.default = Style;
//# sourceMappingURL=Style.js.map