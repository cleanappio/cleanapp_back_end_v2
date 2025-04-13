import { Animated as RNAnimated } from 'react-native';
import { ShapeSource } from '../../components/ShapeSource';
import ImageSource from '../../components/ImageSource';
import FillLayer from '../../components/FillLayer';
import FillExtrusionLayer from '../../components/FillExtrusionLayer';
import LineLayer from '../../components/LineLayer';
import CircleLayer from '../../components/CircleLayer';
import { SymbolLayer } from '../../components/SymbolLayer';
import RasterLayer from '../../components/RasterLayer';
import BackgroundLayer from '../../components/BackgroundLayer';
declare const Animated: {
    ShapeSource: RNAnimated.AnimatedComponent<typeof ShapeSource>;
    ImageSource: RNAnimated.AnimatedComponent<typeof ImageSource>;
    FillLayer: RNAnimated.AnimatedComponent<typeof FillLayer>;
    FillExtrusionLayer: RNAnimated.AnimatedComponent<typeof FillExtrusionLayer>;
    LineLayer: RNAnimated.AnimatedComponent<typeof LineLayer>;
    CircleLayer: RNAnimated.AnimatedComponent<typeof CircleLayer>;
    SymbolLayer: RNAnimated.AnimatedComponent<typeof SymbolLayer>;
    RasterLayer: RNAnimated.AnimatedComponent<typeof RasterLayer>;
    BackgroundLayer: RNAnimated.AnimatedComponent<typeof BackgroundLayer>;
};
export default Animated;
//# sourceMappingURL=Animated.d.ts.map