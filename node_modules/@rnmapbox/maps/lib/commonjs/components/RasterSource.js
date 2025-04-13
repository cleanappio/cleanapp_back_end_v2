"use strict";

Object.defineProperty(exports, "__esModule", {
  value: true
});
exports.default = void 0;
var _react = _interopRequireDefault(require("react"));
var _reactNative = require("react-native");
var _utils = require("../utils");
var _RNMBXRasterSourceNativeComponent = _interopRequireDefault(require("../specs/RNMBXRasterSourceNativeComponent"));
var _AbstractSource = _interopRequireDefault(require("./AbstractSource"));
var _jsxRuntime = require("react/jsx-runtime");
function _interopRequireDefault(e) { return e && e.__esModule ? e : { default: e }; }
const MapboxGL = _reactNative.NativeModules.RNMBXModule;
const isTileTemplateUrl = url => !!url && (url.includes('{z}') || url.includes('{bbox-') || url.includes('{quadkey}'));
/**
 * RasterSource is a map content source that supplies raster image tiles to be shown on the map.
 * The location of and metadata about the tiles are defined either by an option dictionary
 * or by an external file that conforms to the TileJSON specification.
 */
class RasterSource extends _AbstractSource.default {
  static defaultProps = {
    id: MapboxGL.StyleSource.DefaultSourceID
  };
  constructor(props) {
    super(props);
    if (isTileTemplateUrl(props.url)) {
      console.warn(`RasterSource 'url' property contains a Tile URL Template, but is intended for a StyleJSON URL. Please migrate your VectorSource to use: \`tileUrlTemplates=["${props.url}"]\` instead.`);
    }
  }
  render() {
    let {
      url
    } = this.props;
    let {
      tileUrlTemplates
    } = this.props;

    // Swapping url for tileUrlTemplates to provide backward compatibility
    // when RasterSource supported only tile url as url prop
    if (isTileTemplateUrl(url)) {
      tileUrlTemplates = [url];
      url = undefined;
    }
    const props = {
      ...this.props,
      id: this.props.id,
      existing: this.props.existing,
      url,
      tileUrlTemplates,
      minZoomLevel: this.props.minZoomLevel,
      maxZoomLevel: this.props.maxZoomLevel,
      tileSize: this.props.tileSize,
      tms: this.props.tms,
      attribution: this.props.attribution
    };
    return (
      /*#__PURE__*/
      // @ts-expect-error just codegen stuff
      (0, _jsxRuntime.jsx)(_RNMBXRasterSourceNativeComponent.default, {
        ref: this.setNativeRef,
        ...props,
        children: (0, _utils.cloneReactChildrenWithProps)(this.props.children, {
          sourceID: this.props.id
        })
      })
    );
  }
}
var _default = exports.default = RasterSource;
//# sourceMappingURL=RasterSource.js.map