"use strict";

import React from 'react';
import { NativeModules } from 'react-native';
import RNMBXFillExtrusionLayerNativeComponent from '../specs/RNMBXFillExtrusionLayerNativeComponent';
import AbstractLayer from './AbstractLayer';
import { jsx as _jsx } from "react/jsx-runtime";
const MapboxGL = NativeModules.RNMBXModule;
/**
 * FillExtrusionLayer is a style layer that renders one or more 3D extruded polygons on the map.
 */
class FillExtrusionLayer extends AbstractLayer {
  static defaultProps = {
    sourceID: MapboxGL.StyleSource.DefaultSourceID
  };
  render() {
    const props = {
      ...this.props,
      ...this.baseProps,
      sourceLayerID: this.props.sourceLayerID
    };
    return /*#__PURE__*/_jsx(RNMBXFillExtrusionLayerNativeComponent
    // @ts-expect-error just codegen stuff
    , {
      ref: this.setNativeLayer,
      ...props
    });
  }
}
export default FillExtrusionLayer;
//# sourceMappingURL=FillExtrusionLayer.js.map