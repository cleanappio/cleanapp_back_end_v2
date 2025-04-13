"use strict";

Object.defineProperty(exports, "__esModule", {
  value: true
});
exports.ShapeSource = void 0;
var _react = _interopRequireDefault(require("react"));
var _reactNative = require("react-native");
var _RNMBXShapeSourceNativeComponent = _interopRequireDefault(require("../specs/RNMBXShapeSourceNativeComponent"));
var _NativeRNMBXShapeSourceModule = _interopRequireDefault(require("../specs/NativeRNMBXShapeSourceModule"));
var _utils = require("../utils");
var _deprecation = require("../utils/deprecation");
var _AbstractSource = _interopRequireDefault(require("./AbstractSource"));
var _NativeBridgeComponent = _interopRequireDefault(require("./NativeBridgeComponent"));
var _jsxRuntime = require("react/jsx-runtime");
function _interopRequireDefault(e) { return e && e.__esModule ? e : { default: e }; }
const MapboxGL = _reactNative.NativeModules.RNMBXModule;
/**
 * ShapeSource is a map content source that supplies vector shapes to be shown on the map.
 * The shape may be an url or a GeoJSON object
 */
class ShapeSource extends (0, _NativeBridgeComponent.default)(_AbstractSource.default, _NativeRNMBXShapeSourceModule.default) {
  static NATIVE_ASSETS_KEY = 'assets';
  static defaultProps = {
    id: MapboxGL.StyleSource.DefaultSourceID
  };
  constructor(props) {
    super(props);
  }
  _setNativeRef(nativeRef) {
    this.setNativeRef(nativeRef);
    super._runPendingNativeMethods(nativeRef);
  }

  /**
   * Returns the zoom needed to expand the cluster.
   *
   * @example
   * const zoom = await shapeSource.getClusterExpansionZoom(clusterId);
   *
   * @param  {Feature} feature - The feature cluster to expand.
   * @return {number}
   */
  async getClusterExpansionZoom(feature) {
    const res = await this._runNativeMethod('getClusterExpansionZoom', this._nativeRef, [JSON.stringify(feature)]);
    return res.data;
  }

  /**
   * Returns the FeatureCollection from the cluster.
   *
   * @example
   * const collection = await shapeSource.getClusterLeaves(clusterId, limit, offset);
   *
   * @param  {GeoJSON.Feature} feature - The feature cluster to expand.
   * @param  {number} limit - The number of points to return.
   * @param  {number} offset - The amount of points to skip (for pagination).
   * @return {FeatureCollection}
   */
  async getClusterLeaves(feature, limit, offset) {
    const res = await this._runNativeMethod('getClusterLeaves', this._nativeRef, [JSON.stringify(feature), limit, offset]);
    if ((0, _utils.isAndroid)()) {
      return JSON.parse(res.data);
    }
    return res.data;
  }

  /**
   * Returns the FeatureCollection from the cluster (on the next zoom level).
   *
   * @example
   * const collection = await shapeSource.getClusterChildren(clusterId);
   *
   * @param  {GeoJSON.Feature} feature - The feature cluster to expand.
   * @return {FeatureCollection}
   */
  async getClusterChildren(feature) {
    const res = await this._runNativeMethod('getClusterChildren', this._nativeRef, [JSON.stringify(feature)]);
    if ((0, _utils.isAndroid)()) {
      return JSON.parse(res.data);
    }
    return res.data;
  }
  setNativeProps(props) {
    const shallowProps = Object.assign({}, props);

    // Adds support for Animated
    if (shallowProps.shape && typeof shallowProps.shape !== 'string') {
      shallowProps.shape = JSON.stringify(shallowProps.shape);
    }
    super.setNativeProps(shallowProps);
  }
  _getShape() {
    if (!this.props.shape) {
      return;
    }
    return (0, _utils.toJSONString)(this.props.shape);
  }
  _decodePayload(payload) {
    // we check whether the payload is a string, since the strict type safety is enforced only on iOS on the new arch
    // on Android, on both archs, the payload is an object
    if (typeof payload === 'string') {
      return JSON.parse(payload);
    } else {
      return payload;
    }
  }
  onPress(event) {
    const payload = this._decodePayload(event.nativeEvent.payload);
    const {
      features,
      coordinates,
      point
    } = payload;
    let newEvent = {
      features,
      coordinates,
      point
    };
    newEvent = (0, _deprecation.copyPropertiesAsDeprecated)(event, newEvent, key => {
      console.warn(`event.${key} is deprecated on ShapeSource#onPress, please use event.features`);
    }, {
      nativeEvent: origNativeEvent => ({
        ...origNativeEvent,
        payload: features[0]
      })
    });
    this.props.onPress?.(newEvent);
  }
  render() {
    const props = {
      id: this.props.id,
      existing: this.props.existing,
      url: this.props.url,
      shape: this._getShape(),
      hitbox: this.props.hitbox,
      hasPressListener: (0, _utils.isFunction)(this.props.onPress),
      onMapboxShapeSourcePress: this.onPress.bind(this),
      cluster: this.props.cluster ? 1 : 0,
      clusterRadius: this.props.clusterRadius,
      clusterMaxZoomLevel: this.props.clusterMaxZoomLevel,
      clusterProperties: this.props.clusterProperties,
      maxZoomLevel: this.props.maxZoomLevel,
      buffer: this.props.buffer,
      tolerance: this.props.tolerance,
      lineMetrics: this.props.lineMetrics,
      onPress: undefined,
      ref: nativeRef => this._setNativeRef(nativeRef)
    };
    return (
      /*#__PURE__*/
      // @ts-expect-error just codegen stuff
      (0, _jsxRuntime.jsx)(_RNMBXShapeSourceNativeComponent.default, {
        ...props,
        children: (0, _utils.cloneReactChildrenWithProps)(this.props.children, {
          sourceID: this.props.id
        })
      })
    );
  }
}
exports.ShapeSource = ShapeSource;
//# sourceMappingURL=ShapeSource.js.map